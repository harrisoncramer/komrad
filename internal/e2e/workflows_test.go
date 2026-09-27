//go:build integration

package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"komrad/internal/komradtest"
	"komrad/internal/store"
	agentv1 "komrad/proto/gen/agent/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationWorkflowsAgainstRealRepositories walks worktree workflows from their creation over real clones to their deletion.
func TestIntegrationWorkflowsAgainstRealRepositories(t *testing.T) {
	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{WorktreesDir: t.TempDir()})
	ctx := t.Context()

	t.Run("a linked worktree is refused as the cwd, not being the primary checkout", func(t *testing.T) {
		linked := filepath.Join(t.TempDir(), "tree")
		runGitIn(t, initGitRepo(t), "worktree", "add", "--quiet", "-b", "feature", linked)

		_, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
			Name:     "linked",
			Cwd:      linked,
			Worktree: &workflowv1.WorktreeConfig{Enabled: true},
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "primary checkout")
	})

	t.Run("a clone with a blank base stores what its origin/HEAD names", func(t *testing.T) {
		clone := initGitRepo(t)
		runGitIn(t, clone, "checkout", "--quiet", "-b", "wip")

		assertStoredBase(t, instance, assertStoredBaseParams{
			req: &workflowv1.CreateWorkflowRequest{
				Name:     "cloned",
				Cwd:      clone,
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			},
			want: "main",
		})
	})

	t.Run("a repository with no origin stores the branch it is on", func(t *testing.T) {
		dir := t.TempDir()
		runGitIn(t, dir, "init", "--quiet", "-b", "trunk")
		runGitIn(t, dir, "config", "user.email", "test@komrad.test")
		runGitIn(t, dir, "config", "user.name", "komrad test")
		require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644))
		runGitIn(t, dir, "add", "README.md")
		runGitIn(t, dir, "commit", "--quiet", "-m", "initial")

		assertStoredBase(t, instance, assertStoredBaseParams{
			req: &workflowv1.CreateWorkflowRequest{
				Name:     "local",
				Cwd:      dir,
				Worktree: &workflowv1.WorktreeConfig{Enabled: true},
			},
			want: "trunk",
		})
	})

	t.Run("deleting finishes once a slow cleanup catches up", func(t *testing.T) {
		replaceWorktreeTeardown(t, instance, "#!/bin/sh\nsleep 0.2\nexit 0\n")

		workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(buildWorktreeWorkflow(t)))
		require.NoError(t, err)
		workflowID := workflow.Msg.GetWorkflow().GetId()

		created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
			Prompt:     "fix the flaky login test",
			Title:      "login",
			WorkflowId: workflowID,
		}))
		require.NoError(t, err)

		_, err = instance.Workflows.ArchiveWorkflow(ctx, connect.NewRequest(&workflowv1.ArchiveWorkflowRequest{Id: workflowID}))
		require.NoError(t, err)

		_, err = instance.Workflows.DeleteWorkflow(ctx, connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: workflowID}))
		require.NoError(t, err)

		got, err := instance.Stores.Workflows.GetIncludingArchived(ctx, workflowID)
		require.NoError(t, err)
		assert.NotNil(t, got, "the workflow is not gone yet, since its agent's delete has not run")
		instance.WorkQueue()

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			_, err := instance.Stores.Agents.GetByID(ctx, created.Msg.GetAgent().GetId())
			assert.ErrorIs(c, err, store.ErrNotFound, "its agent is gone once the deprovisioner finishes")

			_, err = instance.Stores.Workflows.GetIncludingArchived(ctx, workflowID)
			assert.ErrorIs(c, err, store.ErrNotFound, "and the workflow it emptied out goes with it, with no second delete")
		}, 5*time.Second, 20*time.Millisecond)
	})
}

type assertStoredBaseParams struct {
	req  *workflowv1.CreateWorkflowRequest
	want string
}

func assertStoredBase(t *testing.T, instance *komradtest.Instance, params assertStoredBaseParams) {
	t.Helper()

	created, err := instance.Workflows.CreateWorkflow(t.Context(), connect.NewRequest(params.req))
	require.NoError(t, err)
	assert.Equal(t, params.want, created.Msg.GetWorkflow().GetWorktree().GetBaseBranch(), "the response carries the base stored")

	read, err := instance.Workflows.GetWorkflow(t.Context(), connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: created.Msg.GetWorkflow().GetId()}))
	require.NoError(t, err)
	assert.Equal(t, params.want, read.Msg.GetWorkflow().GetWorktree().GetBaseBranch())
}
