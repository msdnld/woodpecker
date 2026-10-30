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

package pipeline

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	forge_types "go.woodpecker-ci.org/woodpecker/v3/server/forge/types"
	"go.woodpecker-ci.org/woodpecker/v3/server/model"
)

func TestParseWorkflowInputs(t *testing.T) {
	data := []byte(`
inputs:
  environment:
    description: Where to deploy
    type: choice
    required: true
    default: staging
    options: [staging, production]
  version:
    type: string
    default: latest
  dry_run:
    type: boolean
    default: true
  plain: {}

steps:
  - name: deploy
    image: alpine
    commands: [echo hi]
`)
	specs, err := ParseWorkflowInputs(data)
	assert.NoError(t, err)
	// order is preserved
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"environment", "version", "dry_run", "plain"}, names)
	assert.Equal(t, InputTypeChoice, specs[0].Type)
	assert.Equal(t, []string{"staging", "production"}, specs[0].Options)
	// untyped input defaults to string
	assert.Equal(t, InputTypeString, specs[3].Type)

	// no inputs block -> nil
	specs, err = ParseWorkflowInputs([]byte("steps: []\n"))
	assert.NoError(t, err)
	assert.Nil(t, specs)

	// choice without options -> error
	_, err = ParseWorkflowInputs([]byte("inputs:\n  x:\n    type: choice\n"))
	assert.Error(t, err)
}

func TestParseWorkflowInputsListForm(t *testing.T) {
	// list form with the `select` alias for choice
	data := []byte(`
inputs:
  - name: channel
    description: OTA channel
    type: select
    default: preview
    options: [preview, production]
  - name: variant
    type: select
    default: dev
    options: [dev, prod]

steps:
  - name: x
    image: alpine
    commands: [echo hi]
`)
	specs, err := ParseWorkflowInputs(data)
	assert.NoError(t, err)
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"channel", "variant"}, names)
	// `select` is normalised to `choice`
	assert.Equal(t, InputTypeChoice, specs[0].Type)
	assert.Equal(t, InputTypeChoice, specs[1].Type)
	assert.Equal(t, []string{"preview", "production"}, specs[0].Options)

	// list item without a name -> error
	_, err = ParseWorkflowInputs([]byte("inputs:\n  - type: string\n"))
	assert.Error(t, err)
}

func TestValidateDispatchInputs(t *testing.T) {
	specs := []InputSpec{
		{Name: "environment", Type: InputTypeChoice, Required: true, Default: "staging", Options: []string{"staging", "production"}},
		{Name: "version", Type: InputTypeString, Default: "latest"},
		{Name: "dry_run", Type: InputTypeBoolean, Default: true},
		{Name: "count", Type: InputTypeNumber},
		{Name: "token", Type: InputTypeString, Required: true},
	}

	t.Run("defaults + coercion", func(t *testing.T) {
		env, err := validateDispatchInputs(specs, map[string]any{
			"environment": "production",
			"dry_run":     false,
			"count":       3,
			"token":       "abc",
		})
		assert.NoError(t, err)
		assert.Equal(t, map[string]string{
			"CI_INPUT_ENVIRONMENT": "production",
			"CI_INPUT_VERSION":     "latest", // default applied
			"CI_INPUT_DRY_RUN":     "false",
			"CI_INPUT_COUNT":       "3",
			"CI_INPUT_TOKEN":       "abc",
		}, env)
	})

	t.Run("missing required without default", func(t *testing.T) {
		_, err := validateDispatchInputs(specs, map[string]any{"token": ""})
		assert.ErrorIs(t, err, &ErrBadRequest{})
	})

	t.Run("invalid choice", func(t *testing.T) {
		_, err := validateDispatchInputs(specs, map[string]any{"environment": "nope", "token": "x"})
		assert.ErrorIs(t, err, &ErrBadRequest{})
	})

	t.Run("non-numeric number", func(t *testing.T) {
		_, err := validateDispatchInputs(specs, map[string]any{"count": "abc", "token": "x"})
		assert.ErrorIs(t, err, &ErrBadRequest{})
	})

	t.Run("unknown input rejected", func(t *testing.T) {
		_, err := validateDispatchInputs(specs, map[string]any{"token": "x", "bogus": "1"})
		assert.ErrorIs(t, err, &ErrBadRequest{})
	})
}

func TestApplyDispatchInputs(t *testing.T) {
	configs := []*forge_types.FileMeta{
		{Name: ".woodpecker/test.yaml", Data: []byte("steps: {}\n")},
		{Name: ".woodpecker/deploy.yaml", Data: []byte("inputs:\n  version:\n    required: true\n")},
	}

	t.Run("injects inputs of the selected workflow", func(t *testing.T) {
		pl := &model.Pipeline{SelectedWorkflow: "deploy", DispatchInputs: map[string]any{"version": "1.2"}}
		assert.NoError(t, applyDispatchInputs(pl, configs))
		assert.Equal(t, map[string]string{"CI_INPUT_VERSION": "1.2"}, pl.AdditionalVariables)
	})

	for name, pl := range map[string]*model.Pipeline{
		"no selected workflow":    {DispatchInputs: map[string]any{"version": "1.2"}},
		"unknown workflow":        {SelectedWorkflow: "nope", DispatchInputs: map[string]any{"version": "1.2"}},
		"workflow without inputs": {SelectedWorkflow: "test", DispatchInputs: map[string]any{"version": "1.2"}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.True(t, errors.Is(applyDispatchInputs(pl, configs), &ErrBadRequest{}))
		})
	}
}

func TestAttachDispatchInputs(t *testing.T) {
	workflows := []*model.WorkflowInfo{{Name: "deploy"}, {Name: "broken"}, {Name: "test"}}
	attachDispatchInputs(workflows, []*forge_types.FileMeta{
		{Name: ".woodpecker/deploy.yaml", Data: []byte("inputs:\n  version:\n    required: true\n")},
		{Name: ".woodpecker/broken.yaml", Data: []byte("inputs: nope\n")},
		{Name: ".woodpecker/test.yaml", Data: []byte("steps: {}\n")},
		{Name: ".woodpecker/unlisted.yaml", Data: []byte("inputs:\n  x: {}\n")},
	})

	assert.Equal(t, []InputSpec{{Name: "version", Type: InputTypeString, Required: true}}, workflows[0].Inputs)
	assert.Empty(t, workflows[1].Inputs)
	assert.Empty(t, workflows[2].Inputs)
}
