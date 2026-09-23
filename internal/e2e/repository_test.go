//go:build integration

package e2e

import (
	"testing"

	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationCwdSyncRecordsTheRepository covers the identity a pull request is matched on, read off
// a real checkout by the cwd loop: the branch and the canonical origin land on the agent, an
// origin moved by hand is picked up on the next pass, and one that names no forge clears it.
func TestIntegrationCwdSyncRecordsTheRepository(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: t.TempDir()})
	ctx := t.Context()

	repo := initGitRepo(t)
	runGitIn(t, repo, "remote", "set-url", "origin", "git@github.com:harrisoncramer/weave.git")

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "backend",
		Cwd:      repo,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "do the work",
		Title:      "worker",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)
	agentID := created.Msg.GetAgent().GetId()

	instance.SyncCwds()

	synced, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: agentID}))
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/harrisoncramer/weave", synced.Msg.GetAgent().GetAgent().GetRepositoryUrl())
	assert.NotEmpty(t, synced.Msg.GetAgent().GetAgent().GetBranch())

	runGitIn(t, repo, "remote", "set-url", "origin", "https://gitlab.com/group/subgroup/repo.git")
	instance.SyncCwds()

	moved, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: agentID}))
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.com/group/subgroup/repo", moved.Msg.GetAgent().GetAgent().GetRepositoryUrl())

	runGitIn(t, repo, "remote", "set-url", "origin", t.TempDir())
	instance.SyncCwds()

	cleared, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: agentID}))
	require.NoError(t, err)
	assert.Nil(t, cleared.Msg.GetAgent().GetAgent().RepositoryUrl)
	assert.NotEmpty(t, cleared.Msg.GetAgent().GetAgent().GetBranch())
}
