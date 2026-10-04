//go:build integration

package e2e

import (
	"testing"

	"komrad/internal/forge"
	"komrad/internal/komradtest"
	agentv1 "komrad/proto/gen/agent/v1"
	forgev1 "komrad/proto/gen/forge/v1"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationPullRequestEventsReachTheAgent follows an agent's open pull request through its
// checks failing, going green while the forge still blocks merging, and being mergeable at last.
func TestIntegrationPullRequestEventsReachTheAgent(t *testing.T) {
	isolateGit(t)

	fake := startFakeGitHub(t)
	fake.setChecks("IN_PROGRESS")
	fake.setMergeState("BLOCKED")
	instance := komradtest.Start(t, komradtest.Params{
		WorktreesDir:  t.TempDir(),
		ForgeBuilders: map[string]forge.Builder{forge.KindGitHub: buildGitHub(fake.server)},
	})
	ctx := t.Context()

	_, err := instance.Forges.ConnectForge(ctx, connect.NewRequest(&forgev1.ConnectForgeRequest{Host: "github.com", Token: "secret"}))
	require.NoError(t, err)

	repo := initGitRepo(t)
	runGitIn(t, repo, "remote", "set-url", "origin", "git@github.com:acme/widgets.git")
	runGitIn(t, repo, "checkout", "--quiet", "-b", "feature/widgets")

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name: "widgets",
		Cwd:  repo,
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "ship the widgets",
		Title:      "widgets",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)
	agentID := created.Msg.GetAgent().GetId()

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

	t.Run("a rerun going green while the forge still blocks merging says nothing", func(t *testing.T) {
		fake.setChecks("SUCCESS")

		assert.Empty(t, sync(t))

		got, err := instance.Agents.GetAgent(ctx, connect.NewRequest(&agentv1.GetAgentRequest{Id: agentID}))
		require.NoError(t, err)
		readiness := got.Msg.GetPullRequest().GetMergeReadiness()
		assert.Equal(t, pullrequestv1.Checks_CHECKS_PASSING, readiness.GetChecks())
		assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_BLOCKED, readiness.GetMergeability())
	})

	t.Run("the agent hears once the forge would merge its green pull request", func(t *testing.T) {
		fake.setMergeState("CLEAN")

		read := sync(t)
		require.Len(t, read, 1)
		assert.Contains(t, read[0].GetBody(), "checks: passing")
		assert.Contains(t, read[0].GetBody(), "mergeability: clean")
	})

	t.Run("checks rerunning on a mergeable pull request say nothing", func(t *testing.T) {
		fake.setChecks("IN_PROGRESS")

		assert.Empty(t, sync(t))
	})

	t.Run("the agent hears once checks go green on a mergeable pull request", func(t *testing.T) {
		fake.setChecks("SUCCESS")

		read := sync(t)
		require.Len(t, read, 1)
		assert.Contains(t, read[0].GetBody(), "checks: passing")
	})
}
