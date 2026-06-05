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

// This file backs the manual "workflow dispatch" UI (GitHub-Actions style
// workflow selection + typed inputs). It is kept separate from the upstream
// pipeline handlers so the fork stays easy to rebase: only a single route
// registration in router/api.go references it.
package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"go.woodpecker-ci.org/woodpecker/v3/pipeline/frontend/metadata"
	"go.woodpecker-ci.org/woodpecker/v3/pipeline/frontend/yaml"
	"go.woodpecker-ci.org/woodpecker/v3/server"
	"go.woodpecker-ci.org/woodpecker/v3/server/forge"
	forge_types "go.woodpecker-ci.org/woodpecker/v3/server/forge/types"
	"go.woodpecker-ci.org/woodpecker/v3/server/model"
	"go.woodpecker-ci.org/woodpecker/v3/server/pipeline"
	server_metadata "go.woodpecker-ci.org/woodpecker/v3/server/pipeline/metadata"
	"go.woodpecker-ci.org/woodpecker/v3/server/router/middleware/session"
	"go.woodpecker-ci.org/woodpecker/v3/server/store"
)

// WorkflowMeta describes a workflow file available for manual dispatch.
type WorkflowMeta struct {
	Name   string               `json:"name"`             // short selector name (base file name without extension)
	File   string               `json:"file"`             // full config file name as stored in the forge
	Inputs []pipeline.InputSpec `json:"inputs,omitempty"` // declared workflow_dispatch inputs, in file order
} //	@name	WorkflowMeta

// ListWorkflows
//
//	@Summary		List workflows available for manual dispatch
//	@Description	Returns the workflow files configured on the given ref, for the manual run / workflow_dispatch UI.
//	@Router			/repos/{repo_id}/workflows [get]
//	@Produce		json
//	@Success		200	{array}	WorkflowMeta
//	@Tags			Pipelines
//	@Param			Authorization	header	string	true	"Insert your personal access token"	default(Bearer <personal access token>)
//	@Param			repo_id			path	int		true	"the repository id"
//	@Param			ref				query	string	false	"the ref (branch) to read workflows from; defaults to the repo default branch"
func ListWorkflows(c *gin.Context) {
	_store := store.FromContext(c)
	repo := session.Repo(c)
	_forge, err := server.Config.Services.Manager.ForgeFromRepo(repo)
	if err != nil {
		log.Error().Err(err).Msg("Cannot get forge from repo")
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	user := session.User(c)

	ref := c.Query("ref")
	if ref == "" {
		ref = repo.Branch
	}

	repoUser, err := _store.GetUser(repo.UserID)
	if err != nil {
		handleDBError(c, err)
		return
	}

	forge.Refresh(c, _forge, _store, repoUser)

	commit, err := _forge.BranchHead(c, user, repo, ref)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("could not fetch branch head: %w", err))
		return
	}

	// Mirror the manual-trigger pipeline so the config service resolves the same
	// files Create() would fetch for this ref.
	tmpPipeline := &model.Pipeline{
		Event:  model.EventManual,
		Branch: ref,
		Ref:    ref,
		Commit: commit.SHA,
	}

	configService := server.Config.Services.Manager.ConfigServiceFromRepo(repo)
	configs, err := configService.Fetch(c, _forge, repoUser, repo, tmpPipeline, nil, false)
	if err != nil {
		_ = c.AbortWithError(http.StatusInternalServerError, fmt.Errorf("could not fetch workflow configs: %w", err))
		return
	}

	// Only list workflows that would actually run on a manual dispatch, i.e.
	// whose `when` matches the manual event (workflows with no event filter
	// match too). This hides push/tag/cron-only workflows that cannot be
	// dispatched manually and would otherwise error if selected.
	dispatchMeta := server_metadata.NewServerMetadata(_forge, repo, tmpPipeline, nil, server.Config.Server.Host).GetWorkflowMetadata(nil)

	workflows := make([]*WorkflowMeta, 0, len(configs))
	for _, cfg := range configs {
		if !workflowRunsOnManual(cfg, dispatchMeta) {
			continue
		}
		workflows = append(workflows, newWorkflowMeta(cfg))
	}

	c.JSON(http.StatusOK, workflows)
}

// workflowRunsOnManual reports whether a workflow file would be included in a
// manual dispatch run, evaluating its top-level `when` against manual-event
// metadata exactly like the compiler does. It fails open: a file that cannot be
// parsed or evaluated is kept in the list so a real workflow is never hidden by
// a parsing quirk on our side.
func workflowRunsOnManual(cfg *forge_types.FileMeta, dispatchMeta metadata.Metadata) bool {
	workflow, err := yaml.ParseBytes(cfg.Data)
	if err != nil {
		log.Warn().Err(err).Str("workflow", cfg.Name).Msg("could not parse workflow for manual dispatch filter")
		return true
	}
	match, err := workflow.When.Match(dispatchMeta, true, nil)
	if err != nil {
		log.Warn().Err(err).Str("workflow", cfg.Name).Msg("could not evaluate workflow when for manual dispatch filter")
		return true
	}
	return match
}

func newWorkflowMeta(cfg *forge_types.FileMeta) *WorkflowMeta {
	inputs, err := pipeline.ParseWorkflowInputs(cfg.Data)
	if err != nil {
		// a malformed inputs block should not hide the workflow from the list;
		// surface it without inputs so the user can still run it.
		log.Warn().Err(err).Str("workflow", cfg.Name).Msg("could not parse workflow inputs")
		inputs = nil
	}
	return &WorkflowMeta{
		Name:   pipeline.DispatchWorkflowName(cfg.Name),
		File:   cfg.Name,
		Inputs: inputs,
	}
}
