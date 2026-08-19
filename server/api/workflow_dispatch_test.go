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

package api

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.woodpecker-ci.org/woodpecker/v3/pipeline/frontend/metadata"
	forge_types "go.woodpecker-ci.org/woodpecker/v3/server/forge/types"
)

func manualMeta() metadata.Metadata {
	return metadata.Metadata{
		Curr: metadata.Pipeline{Event: metadata.EventManual},
	}
}

func TestWorkflowRunsOnManual(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want bool
	}{
		{
			name: "no when block",
			yaml: "steps:\n  a:\n    image: alpine\n",
			want: true,
		},
		{
			name: "manual event",
			yaml: "when:\n  - event: manual\nsteps:\n  a:\n    image: alpine\n",
			want: true,
		},
		{
			name: "push only",
			yaml: "when:\n  - event: push\nsteps:\n  a:\n    image: alpine\n",
			want: false,
		},
		{
			// `evaluate` references CI_INPUT_* vars that only exist after the
			// dispatch form is submitted; it must not hide the workflow here.
			name: "manual event gated by input evaluate",
			yaml: "when:\n  - event: manual\n    evaluate: 'CI_INPUT_TARGET == \"mobile-ota\"'\nsteps:\n  a:\n    image: alpine\n",
			want: true,
		},
		{
			name: "unparsable fails open",
			yaml: "steps: [",
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &forge_types.FileMeta{Name: ".woodpecker/test.yaml", Data: []byte(tt.yaml)}
			assert.Equal(t, tt.want, workflowRunsOnManual(cfg, manualMeta()))
		})
	}
}
