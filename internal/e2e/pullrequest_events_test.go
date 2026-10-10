//go:build integration

package e2e

import (
	"testing"

	"komrad/internal/forge"
	"komrad/internal/komradtest"
	agentv1 "komrad/proto/gen/agent/v1"
	forgev1 "komrad/proto/gen/forge/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationPullRequestChecksReachTheAgent follows an agent's open pull request on GitHub through its checks failing and then going green.
func TestIntegrationPullRequestChecksReachTheAgent(t *testing.T) {
	isolateGit(t)

	fake := startFakeGitHub(t)
	fake.setChecks("IN_PROGRESS")
	instance := komradtest.Start(t, komradtest.Params{
		WorktreesDir:  t.TempDir(),
		ForgeBuilders: map[string]forge.Builder{forge.KindGitHub: buildGitHub(fake.server)},
	})
	ctx := t.Context()

	_, err := instance.Forges.ConnectForge(ctx, connect.NewRequest(&forgev1.ConnectForgeRequest{Host: "github.com", Token: "secret"}))
	require.NoError(t, err)

	repo := initGitRepo(t)
	runGitIn(t, repo, "remote", "set-url", "origin", "git@github.com:acme/widgets.git")
	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "widgets",
		Cwd:      repo,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "ship the widgets",
		Title:      "widgets",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)
	agentID := created.Msg.GetAgent().GetId()
	runGitIn(t, created.Msg.GetAgent().GetCwd(), "checkout", "--quiet", "-b", "feature/widgets")

	instance.SyncCwds()
	instance.SyncPullRequests()
	instance.WorkQueue()
	readMail(t, instance, agentID)

	sync := func(t *testing.T) []*agentv1.Mail {
		t.Helper()

		instance.SyncPullRequests()
		instance.WorkQueue()

		return readMail(t, instance, agentID)
	}

	t.Run("failing checks reach the agent", func(t *testing.T) {
		fake.setChecks("FAILURE")

		read := sync(t)
		require.Len(t, read, 1)
		assert.Contains(t, read[0].GetBody(), "checks: failing")
	})

	t.Run("a rerun going green reaches the agent", func(t *testing.T) {
		fake.setChecks("SUCCESS")

		read := sync(t)
		require.Len(t, read, 1)
		assert.Contains(t, read[0].GetBody(), "checks: passing")
	})
}
