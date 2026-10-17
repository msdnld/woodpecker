// Copyright 2026 Woodpecker Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
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

	"go.woodpecker-ci.org/woodpecker/v3/server/model"
)

func TestCreateTmpPipelineMessage(t *testing.T) {
	commit := &model.Commit{SHA: "abc"}
	user := &model.User{Login: "me"}

	t.Run("no params", func(t *testing.T) {
		pl := createTmpPipeline(model.EventManual, commit, user, &model.PipelineOptions{Branch: "main"})
		assert.Equal(t, "MANUAL PIPELINE @ main", pl.Message)
	})

	t.Run("selected workflow names the run", func(t *testing.T) {
		pl := createTmpPipeline(model.EventManual, commit, user, &model.PipelineOptions{
			Branch:    "main",
			Workflows: []string{".woodpecker/deploy.yaml", "lint"},
			Inputs:    map[string]any{"version": "1.2"},
		})
		assert.Equal(t, "deploy, lint", pl.Message)
	})

	t.Run("custom message", func(t *testing.T) {
		pl := createTmpPipeline(model.EventManual, commit, user, &model.PipelineOptions{
			Branch:  "main",
			Message: "hotfix",
		})
		assert.Equal(t, "MANUAL: hotfix @ main", pl.Message)
	})

	t.Run("workflow with custom message", func(t *testing.T) {
		pl := createTmpPipeline(model.EventManual, commit, user, &model.PipelineOptions{
			Branch:    "main",
			Message:   "hotfix",
			Workflows: []string{"deploy"},
		})
		assert.Equal(t, "deploy: hotfix", pl.Message)
	})
}
