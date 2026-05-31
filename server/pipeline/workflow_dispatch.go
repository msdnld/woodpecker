// Copyright 2024 Woodpecker Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This file implements the manual "workflow dispatch" feature (GitHub-Actions
// style workflow selection). It is intentionally kept in its own file so the
// fork stays easy to rebase against upstream: the only change to existing code
// is a single guarded call to filterDispatchWorkflows in create.go.
package pipeline

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	forge_types "go.woodpecker-ci.org/woodpecker/v3/server/forge/types"
	"go.woodpecker-ci.org/woodpecker/v3/server/model"
)

// Input types supported by a workflow_dispatch `inputs:` block (mirrors GitHub).
const (
	InputTypeString  = "string"
	InputTypeNumber  = "number"
	InputTypeBoolean = "boolean"
	InputTypeChoice  = "choice"

	// InputTypeSelect is accepted as an alias for choice (friendlier wording);
	// it is normalised to choice during parsing so the UI/validation only ever
	// see one canonical type.
	InputTypeSelect = "select"

	// DispatchInputEnvPrefix is prepended to every injected input env var so
	// inputs cannot collide with secrets or existing CI_* variables.
	DispatchInputEnvPrefix = "CI_INPUT_"
)

// InputSpec mirrors a single GitHub-Actions style workflow_dispatch input
// declaration parsed from a workflow file's top-level `inputs:` block.
type InputSpec struct {
	Name        string   `json:"name"        yaml:"name"`
	Description string   `json:"description,omitempty" yaml:"description"`
	Type        string   `json:"type,omitempty"        yaml:"type"` // string|number|boolean|choice
	Required    bool     `json:"required,omitempty"    yaml:"required"`
	Default     any      `json:"default,omitempty"     yaml:"default"`
	Options     []string `json:"options,omitempty"     yaml:"options"`
}

// ParseWorkflowInputs extracts the ordered `inputs:` declarations from a
// workflow file. Order is preserved (mirrors GitHub). Two shapes are accepted:
//
//	inputs:                      inputs:
//	  channel:           or        - name: channel
//	    type: choice                 type: choice
//
// Returns nil when the file declares no inputs.
func ParseWorkflowInputs(data []byte) ([]InputSpec, error) {
	var doc struct {
		Inputs yaml.Node `yaml:"inputs"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	switch doc.Inputs.Kind {
	case 0:
		return nil, nil
	case yaml.MappingNode:
		return parseInputsMapping(doc.Inputs.Content)
	case yaml.SequenceNode:
		return parseInputsSequence(doc.Inputs.Content)
	default:
		return nil, fmt.Errorf("inputs must be a mapping or a list")
	}
}

// parseInputsMapping handles the GitHub map form: `inputs.<name>: {...}`.
func parseInputsMapping(content []*yaml.Node) ([]InputSpec, error) {
	specs := make([]InputSpec, 0, len(content)/2)
	for i := 0; i+1 < len(content); i += 2 {
		name := content[i].Value
		var spec InputSpec
		if err := content[i+1].Decode(&spec); err != nil {
			return nil, fmt.Errorf("invalid input %q: %w", name, err)
		}
		spec.Name = name
		if err := normalizeInputSpec(&spec); err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// parseInputsSequence handles the list form: `inputs: - name: <name> ...`.
func parseInputsSequence(content []*yaml.Node) ([]InputSpec, error) {
	specs := make([]InputSpec, 0, len(content))
	for _, item := range content {
		var spec InputSpec
		if err := item.Decode(&spec); err != nil {
			return nil, fmt.Errorf("invalid input: %w", err)
		}
		if err := normalizeInputSpec(&spec); err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// normalizeInputSpec fills defaults, maps the `select` alias to `choice`, and
// validates the declaration.
func normalizeInputSpec(spec *InputSpec) error {
	if spec.Name == "" {
		return fmt.Errorf("input is missing a name")
	}
	if spec.Type == "" {
		spec.Type = InputTypeString
	}
	if spec.Type == InputTypeSelect {
		spec.Type = InputTypeChoice
	}
	if spec.Type == InputTypeChoice && len(spec.Options) == 0 {
		return fmt.Errorf("input %q is a choice but declares no options", spec.Name)
	}
	return nil
}

// applyDispatchInputs validates the user-submitted inputs against the selected
// workflow's `inputs:` schema and injects them as CI_INPUT_* env vars (which
// flow into every step of the run via AdditionalVariables). Inputs require
// exactly one selected workflow, since the schema is per workflow file.
func applyDispatchInputs(pipeline *model.Pipeline, configs []*forge_types.FileMeta) error {
	if len(configs) != 1 {
		return &ErrBadRequest{Msg: "inputs can only be used when dispatching exactly one workflow"}
	}

	specs, err := ParseWorkflowInputs(configs[0].Data)
	if err != nil {
		return &ErrBadRequest{Msg: fmt.Sprintf("could not parse inputs of %s: %v", configs[0].Name, err)}
	}
	if len(specs) == 0 {
		return &ErrBadRequest{Msg: fmt.Sprintf("workflow %s does not declare any inputs", configs[0].Name)}
	}

	env, err := validateDispatchInputs(specs, pipeline.DispatchInputs)
	if err != nil {
		return err
	}

	if pipeline.AdditionalVariables == nil {
		pipeline.AdditionalVariables = make(map[string]string, len(env))
	}
	maps.Copy(pipeline.AdditionalVariables, env)
	return nil
}

// validateDispatchInputs checks submitted values against the input specs,
// applies defaults, enforces required + choice constraints, and returns the
// CI_INPUT_* env vars to inject.
func validateDispatchInputs(specs []InputSpec, submitted map[string]any) (map[string]string, error) {
	env := make(map[string]string, len(specs))
	known := make(map[string]struct{}, len(specs))

	for _, spec := range specs {
		known[spec.Name] = struct{}{}

		raw, ok := submitted[spec.Name]
		if !ok || raw == nil || raw == "" {
			if spec.Default == nil {
				if spec.Required {
					return nil, &ErrBadRequest{Msg: fmt.Sprintf("input %q is required", spec.Name)}
				}
				continue
			}
			raw = spec.Default
		}

		val, err := coerceInput(spec, raw)
		if err != nil {
			return nil, &ErrBadRequest{Msg: err.Error()}
		}
		env[DispatchInputEnvPrefix+strings.ToUpper(spec.Name)] = val
	}

	// reject unknown inputs to catch typos, mirroring GitHub's behaviour.
	for name := range submitted {
		if _, ok := known[name]; !ok {
			return nil, &ErrBadRequest{Msg: fmt.Sprintf("unknown input %q", name)}
		}
	}

	return env, nil
}

// coerceInput converts a submitted value to its canonical string form per the
// declared type, validating choice membership.
func coerceInput(spec InputSpec, raw any) (string, error) {
	switch spec.Type {
	case InputTypeBoolean:
		switch v := raw.(type) {
		case bool:
			return strconv.FormatBool(v), nil
		case string:
			b, err := strconv.ParseBool(v)
			if err != nil {
				return "", fmt.Errorf("input %q must be a boolean", spec.Name)
			}
			return strconv.FormatBool(b), nil
		default:
			return "", fmt.Errorf("input %q must be a boolean", spec.Name)
		}

	case InputTypeNumber:
		switch v := raw.(type) {
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64), nil
		case int:
			return strconv.Itoa(v), nil
		case int64:
			return strconv.FormatInt(v, 10), nil
		case string:
			if _, err := strconv.ParseFloat(v, 64); err != nil {
				return "", fmt.Errorf("input %q must be a number", spec.Name)
			}
			return v, nil
		default:
			return "", fmt.Errorf("input %q must be a number", spec.Name)
		}

	case InputTypeChoice:
		s := fmt.Sprintf("%v", raw)
		if !slices.Contains(spec.Options, s) {
			return "", fmt.Errorf("input %q must be one of %v", spec.Name, spec.Options)
		}
		return s, nil

	case InputTypeString:
		return fmt.Sprintf("%v", raw), nil

	default:
		return "", fmt.Errorf("input %q has unsupported type %q", spec.Name, spec.Type)
	}
}

// DispatchWorkflowName returns the canonical short selector for a workflow
// config file: its base name without extension (".woodpecker/deploy.yaml" ->
// "deploy"). Shared with the API layer so the list endpoint and the filter
// agree on naming.
func DispatchWorkflowName(fileName string) string {
	base := filepath.Base(fileName)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// filterDispatchWorkflows keeps only the config files selected by a manual
// workflow_dispatch run. A selector matches a file when it equals the full
// config file name, its base name, or its base name without extension.
//
// It returns ErrBadRequest if any selector matches no file, mirroring GitHub's
// behaviour of dispatching exactly the chosen workflow(s) rather than silently
// running nothing.
func filterDispatchWorkflows(files []*forge_types.FileMeta, selectors []string) ([]*forge_types.FileMeta, error) {
	result := make([]*forge_types.FileMeta, 0, len(selectors))
	for _, sel := range selectors {
		idx := slices.IndexFunc(files, func(f *forge_types.FileMeta) bool {
			return f.Name == sel || filepath.Base(f.Name) == sel || DispatchWorkflowName(f.Name) == sel
		})
		if idx < 0 {
			return nil, &ErrBadRequest{Msg: fmt.Sprintf("workflow %q does not exist on the selected ref", sel)}
		}
		if !slices.Contains(result, files[idx]) {
			result = append(result, files[idx])
		}
	}
	return result, nil
}
