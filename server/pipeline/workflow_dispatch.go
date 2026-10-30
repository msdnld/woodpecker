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

// This file implements typed workflow_dispatch inputs on top of upstream's
// workflow selection. It is intentionally kept in its own file so the fork stays
// easy to rebase against upstream.
package pipeline

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/rs/zerolog/log"
	"go.yaml.in/yaml/v3"

	"go.woodpecker-ci.org/woodpecker/v3/pipeline/frontend/builder"
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

// InputSpec is the input declaration shared with the workflow list API.
type InputSpec = model.InputSpec

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
// flow into every step of the run via AdditionalVariables). Inputs require a
// selected workflow, since the schema is per workflow file.
func applyDispatchInputs(pipeline *model.Pipeline, configs []*forge_types.FileMeta) error {
	if pipeline.SelectedWorkflow == "" {
		return &ErrBadRequest{Msg: "inputs can only be used when dispatching a selected workflow"}
	}

	selected := builder.SanitizePath(pipeline.SelectedWorkflow)
	idx := slices.IndexFunc(configs, func(f *forge_types.FileMeta) bool {
		return builder.SanitizePath(f.Name) == selected
	})
	if idx < 0 {
		return &ErrBadRequest{Msg: fmt.Sprintf("workflow %q does not exist on the selected ref", pipeline.SelectedWorkflow)}
	}
	config := configs[idx]

	specs, err := ParseWorkflowInputs(config.Data)
	if err != nil {
		return &ErrBadRequest{Msg: fmt.Sprintf("could not parse inputs of %s: %v", config.Name, err)}
	}
	if len(specs) == 0 {
		return &ErrBadRequest{Msg: fmt.Sprintf("workflow %s does not declare any inputs", config.Name)}
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

// attachDispatchInputs fills the declared inputs of listed workflows. A malformed
// inputs block does not hide the workflow; it is listed without inputs.
func attachDispatchInputs(workflows []*model.WorkflowInfo, configs []*forge_types.FileMeta) {
	for _, config := range configs {
		name := builder.SanitizePath(config.Name)
		idx := slices.IndexFunc(workflows, func(w *model.WorkflowInfo) bool { return w.Name == name })
		if idx < 0 {
			continue
		}
		inputs, err := ParseWorkflowInputs(config.Data)
		if err != nil {
			log.Warn().Err(err).Str("workflow", config.Name).Msg("could not parse workflow inputs")
			continue
		}
		workflows[idx].Inputs = inputs
	}
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
