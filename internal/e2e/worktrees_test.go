//go:build integration

package e2e

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	worktreev1 "weave/proto/gen/worktree/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestWorktreesAreListedWithTheirAgentAndReadThroughGit covers the service listing a
// worktree it cut under the agent it cut it for, and reading it back against its base.
func TestWorktreesAreListedWithTheirAgentAndReadThroughGit(t *testing.T) {
	hermeticGit(t)

	worktreesDir := t.TempDir()
	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: worktreesDir})
	ctx := t.Context()

	repo := initGitRepo(t)
	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "backend",
		Cwd:      repo,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	}))
	require.NoError(t, err)

	agent, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "do the work",
		Title:      "lister",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)
	tree := agent.Msg.GetAgent().GetCwd()
	require.NoError(t, os.WriteFile(filepath.Join(tree, "work.txt"), []byte("done\n"), 0o644))
	runGitIn(t, tree, "add", "work.txt")
	runGitIn(t, tree, "commit", "--quiet", "-m", "the agent's work")

	listed, err := instance.Worktrees.ListWorktrees(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	require.Len(t, listed.Msg.GetWorktrees(), 1)
	found := listed.Msg.GetWorktrees()[0]
	assert.Equal(t, tree, found.GetDir())
	assert.True(t, found.GetReadable())
	assert.Equal(t, "agent/"+agent.Msg.GetAgent().GetId(), found.GetBranch())
	assert.Equal(t, agent.Msg.GetAgent().GetId(), found.GetAgent().GetId())
	assert.Equal(t, "lister", found.GetAgent().GetName())
	assert.Contains(t, listed.Msg.GetDirs(), worktreesDir)

	read, err := instance.Worktrees.GetWorktree(ctx, connect.NewRequest(&worktreev1.GetWorktreeRequest{Dir: tree}))
	require.NoError(t, err)

	assert.Equal(t, "origin/main", read.Msg.GetBaseRef())
	assert.Equal(t, int32(1), read.Msg.GetAhead())
	assert.Equal(t, int32(0), read.Msg.GetBehind())
	assert.True(t, read.Msg.GetKeepsBranch(), "the agent's commit is on no other ref")
	require.Len(t, read.Msg.GetCommits(), 1)
	assert.Equal(t, "the agent's work", read.Msg.GetCommits()[0].GetSubject())
	assert.Positive(t, read.Msg.GetSizeBytes())
	assert.NotEmpty(t, read.Msg.GetEntries())

	refused, err := instance.Worktrees.RemoveUnownedWorktrees(ctx, connect.NewRequest(&worktreev1.RemoveUnownedWorktreesRequest{Dirs: []string{tree}}))
	require.NoError(t, err)
	assert.Empty(t, refused.Msg.GetRemoved())
	require.Len(t, refused.Msg.GetRefused(), 1)
	assert.Contains(t, refused.Msg.GetRefused()[0].GetReason(), "lister")
	assert.DirExists(t, tree)
}

// TestDeletingAnAgentTakesItsWorktreeOutOfTheListing covers the agent delete being what
// removes a directory, and an orphan left beside it being the remover's to take.
func TestDeletingAnAgentTakesItsWorktreeOutOfTheListing(t *testing.T) {
	hermeticGit(t)

	worktreesDir := t.TempDir()
	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: worktreesDir})
	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "backend",
		Cwd:      initGitRepo(t),
		Worktree: &workflowv1.WorktreeConfig{Enabled: true},
	}))
	require.NoError(t, err)

	agent, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "do the work",
		Title:      "leaver",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)
	tree := agent.Msg.GetAgent().GetCwd()

	orphan := filepath.Join(worktreesDir, "01a0ffff-0000-7000-8000-000000000000")
	require.NoError(t, os.MkdirAll(orphan, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(orphan, ".git"), []byte("gitdir: /nowhere/.git/worktrees/gone\n"), 0o644))

	_, err = instance.Agents.DeleteAgents(ctx, connect.NewRequest(&agentv1.DeleteAgentsRequest{Ids: []string{agent.Msg.GetAgent().GetId()}}))
	require.NoError(t, err)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		listed, err := instance.Worktrees.ListWorktrees(ctx, connect.NewRequest(&emptypb.Empty{}))
		require.NoError(c, err)
		dirs := make([]string, 0, len(listed.Msg.GetWorktrees()))
		for _, found := range listed.Msg.GetWorktrees() {
			dirs = append(dirs, found.GetDir())
		}
		assert.Equal(c, []string{orphan}, dirs)
	}, 10*time.Second, 100*time.Millisecond)
	assert.NoDirExists(t, tree)

	removed, err := instance.Worktrees.RemoveUnownedWorktrees(ctx, connect.NewRequest(&worktreev1.RemoveUnownedWorktreesRequest{Dirs: []string{orphan}}))
	require.NoError(t, err)
	assert.Equal(t, []string{orphan}, removed.Msg.GetRemoved())
	assert.NoDirExists(t, orphan)
}
