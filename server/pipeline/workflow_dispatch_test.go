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
)

func TestDispatchWorkflowName(t *testing.T) {
	assert.Equal(t, "deploy", DispatchWorkflowName(".woodpecker/deploy.yaml"))
	assert.Equal(t, "deploy", DispatchWorkflowName("deploy.yml"))
	assert.Equal(t, ".woodpecker", DispatchWorkflowName(".woodpecker.yml"))
}

func TestFilterDispatchWorkflows(t *testing.T) {
	files := []*forge_types.FileMeta{
		{Name: ".woodpecker/deploy.yaml"},
		{Name: ".woodpecker/test.yaml"},
		{Name: ".woodpecker/lint.yaml"},
	}

	tests := []struct {
		name      string
		selectors []string
		want      []string
		wantErr   bool
	}{
		{name: "by full name", selectors: []string{".woodpecker/deploy.yaml"}, want: []string{".woodpecker/deploy.yaml"}},
		{name: "by base name", selectors: []string{"test.yaml"}, want: []string{".woodpecker/test.yaml"}},
		{name: "by short name", selectors: []string{"lint"}, want: []string{".woodpecker/lint.yaml"}},
		{name: "multiple", selectors: []string{"deploy", "test"}, want: []string{".woodpecker/deploy.yaml", ".woodpecker/test.yaml"}},
		{name: "dedupe", selectors: []string{"deploy", "deploy.yaml"}, want: []string{".woodpecker/deploy.yaml"}},
		{name: "unknown", selectors: []string{"nope"}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := filterDispatchWorkflows(files, tc.selectors)
			if tc.wantErr {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, &ErrBadRequest{}))
				return
			}
			assert.NoError(t, err)
			names := make([]string, 0, len(got))
			for _, f := range got {
				names = append(names, f.Name)
			}
			assert.Equal(t, tc.want, names)
		})
	}
}
