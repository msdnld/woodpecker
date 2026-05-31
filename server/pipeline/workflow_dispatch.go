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
	"path/filepath"
	"slices"
	"strings"

	forge_types "go.woodpecker-ci.org/woodpecker/v3/server/forge/types"
)

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
