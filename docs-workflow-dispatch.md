# Manual Workflow Dispatch — Contract

GitHub-Actions style manual dispatch for this fork: pick one (or several) workflow
file(s) and run them on demand with typed, validated inputs. Inputs are injected
into every step as `CI_INPUT_<NAME>` environment variables.

The feature is intentionally isolated so the fork stays easy to rebase against
upstream. The only edits to existing code are a single route registration
(`server/router/api.go`) and one guarded call in `server/pipeline/create.go`. All
the rest lives in dedicated `workflow_dispatch.go` files.

---

## 1. Declaring inputs in a workflow file

A workflow file may declare a top-level `inputs:` block. Two shapes are accepted;
both produce the same ordered list (order is preserved, mirroring GitHub).

**Map form** (input name is the key):

```yaml
inputs:
  environment:
    description: Target environment
    type: choice
    required: true
    options: [staging, production]
  replicas:
    type: number
    default: 2
  dry_run:
    type: boolean
    default: true

steps:
  deploy:
    image: alpine
    commands:
      - echo "env=$CI_INPUT_ENVIRONMENT replicas=$CI_INPUT_REPLICAS dry=$CI_INPUT_DRY_RUN"
```

**List form** (input name is the `name:` field):

```yaml
inputs:
  - name: environment
    type: choice
    options: [staging, production]
    required: true
  - name: replicas
    type: number
    default: 2
```

### Input spec fields

| Field         | Type       | Notes                                                                 |
|---------------|------------|-----------------------------------------------------------------------|
| `name`        | string     | Required in list form; ignored in map form (the key wins).            |
| `description` | string     | Optional. Shown in the dispatch UI.                                   |
| `type`        | string     | One of `string` (default), `number`, `boolean`, `choice`. `select` is accepted as an alias for `choice`. |
| `required`    | boolean    | If true and no value is submitted and there is no default → error.   |
| `default`     | any        | Used when the submitted value is absent/empty.                       |
| `options`     | string[]   | Required when `type` is `choice`/`select`; the submitted value must be a member. |

Notes:
- The `inputs:` block is parsed independently of the upstream YAML compiler, so
  unknown top-level keys are ignored by the normal pipeline parser — no runtime
  schema changes are needed. `schema.json` gains an `inputs` definition for IDE
  autocomplete + linting only.
- A malformed `inputs:` block does not hide the workflow from the dispatch list;
  it is surfaced without inputs so the workflow can still be run.

---

## 2. Workflow selector naming

A workflow is selected by name. A selector matches a config file when it equals:
- the full config file name as stored in the forge (e.g. `.woodpecker/deploy.yaml`), or
- its base name (e.g. `deploy.yaml`), or
- its base name without extension — the **canonical short name** (e.g. `deploy`).

The list endpoint returns this short name as `name` and the full path as `file`.
If a selector matches no file on the ref, the request fails (mirrors GitHub:
dispatch the chosen workflow or error, never silently run nothing).

---

## 3. HTTP API

### List dispatchable workflows

```
GET /api/repos/{repo_id}/workflows?ref={ref}
```

- Auth: must have push permission on the repo.
- `ref` (query, optional): branch to read workflows from. Defaults to the repo
  default branch.
- Resolves workflows exactly as a manual run would for that ref (same config
  service, `EventManual`).

Response `200` — array of `WorkflowMeta`:

```jsonc
[
  {
    "name": "deploy",                       // short selector name
    "file": ".woodpecker/deploy.yaml",      // full config file name
    "inputs": [                              // declared inputs, in file order (omitted if none)
      { "name": "environment", "type": "choice", "required": true, "options": ["staging", "production"] },
      { "name": "replicas", "type": "number", "default": 2 },
      { "name": "dry_run", "type": "boolean", "default": true }
    ]
  }
]
```

### Trigger a dispatch run

Uses the existing pipeline-create endpoint with two optional fields:

```
POST /api/repos/{repo_id}/pipelines
```

Body (`PipelineOptions`):

```jsonc
{
  "branch": "main",
  "variables": { "FOO": "bar" },     // existing: extra pipeline variables
  "workflows": ["deploy"],            // optional: restrict run to these workflow selectors
  "inputs": {                          // optional: typed workflow_dispatch inputs
    "environment": "production",
    "replicas": 3,
    "dry_run": false
  }
}
```

Field semantics:
- `workflows` — empty/omitted means "all workflows matching the event" (original
  behaviour). Each entry is a selector (see §2).
- `inputs` — typed values keyed by input name. **Inputs require exactly one
  selected workflow**, since the schema is per workflow file. Submitting inputs
  with zero or multiple selected workflows is an error.

Both fields are transient request-only data — they are never persisted on the
pipeline record.

---

## 4. Server-side validation

When `inputs` is present, each input is validated against the selected workflow's
`inputs:` schema before the pipeline runs. On any violation the just-created
pipeline row is deleted and a `400 Bad Request` is returned.

Validation rules:
- **Exactly one workflow** must be selected, else: `inputs can only be used when dispatching exactly one workflow`.
- The workflow **must declare inputs**, else: `workflow <file> does not declare any inputs`.
- **Required**: a required input with no value and no default → `input "<name>" is required`.
- **Defaults**: missing/empty value falls back to the declared `default`.
- **Unknown inputs** (submitted but not declared) are rejected → `unknown input "<name>"` (catches typos, mirrors GitHub).
- **Type coercion** (the submitted value is coerced to its canonical string form):
  - `boolean` — accepts a JSON bool or a parseable string (`true`/`false`/`1`/`0`…); stored as `"true"`/`"false"`.
  - `number` — accepts a JSON number or a numeric string; stored as its string form.
  - `choice` — stringified value must be one of `options`, else error.
  - `string` — stored as-is (stringified).

---

## 5. Injection into the run

Each validated input becomes an environment variable available to **every step**
of the dispatched run (via the pipeline's `AdditionalVariables`):

```
CI_INPUT_<UPPERCASE_NAME> = <canonical string value>
```

Examples for the body above:

```
CI_INPUT_ENVIRONMENT=production
CI_INPUT_REPLICAS=3
CI_INPUT_DRY_RUN=false
```

The `CI_INPUT_` prefix guarantees inputs cannot collide with secrets or existing
`CI_*` metadata variables. Inputs that are absent and have no default inject no
variable at all.

---

## 6. Source map

| Concern                                   | Location                                          |
|-------------------------------------------|---------------------------------------------------|
| Input parsing, validation, coercion, filter | `server/pipeline/workflow_dispatch.go`           |
| List endpoint + `WorkflowMeta`            | `server/api/workflow_dispatch.go`                 |
| Route registration                        | `server/router/api.go` (`GET /workflows`)         |
| Request fields (`workflows`, `inputs`)    | `server/model/pipeline.go` (`PipelineOptions`)    |
| Transient pipeline fields                 | `server/model/pipeline.go` (`Pipeline`)           |
| Guarded apply on create                   | `server/pipeline/create.go`                       |
| JSON schema (`inputs` definition)         | `pipeline/frontend/yaml/linter/schema/schema.json`|
| Frontend types                            | `web/src/lib/api/types/workflow.ts`               |
| Frontend API client                       | `web/src/lib/api/index.ts`                        |
| Dispatch UI                               | `web/src/views/repo/RepoWorkflowDispatch.vue`     |
