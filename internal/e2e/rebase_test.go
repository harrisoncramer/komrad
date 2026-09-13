package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeIn writes a file into a checkout.
func writeIn(t *testing.T, dir, name, body string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
}

// rebaseAgent is an agent on a worktree of its own, with one commit of its own work on its
// branch.
func rebaseAgent(t *testing.T, instance *weavetest.Instance, repo string) (agentID, tree string) {
	t.Helper()

	ctx := t.Context()

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "backend",
		Cwd:      repo,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	}))
	require.NoError(t, err)

	agent, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "do the work",
		Title:      "rebaser",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	tree = agent.Msg.GetAgent().GetCwd()
	require.DirExists(t, tree)

	writeIn(t, tree, "own.txt", "a first draft\n")
	runGitIn(t, tree, "add", "own.txt")
	runGitIn(t, tree, "commit", "--quiet", "-m", "a first draft")

	writeIn(t, tree, "own.txt", "the agent's work\n")
	runGitIn(t, tree, "add", "own.txt")
	runGitIn(t, tree, "commit", "--quiet", "-m", "the agent's work")

	return agent.Msg.GetAgent().GetId(), tree
}

// landOnTheBase puts the finished content of the agent's file on main as a commit of its own,
// the way a squash merge of its branch would.
func landOnTheBase(t *testing.T, repo string) {
	t.Helper()

	branch := readGitOutput(t, repo, "rev-parse", "--abbrev-ref", "HEAD")
	runGitIn(t, repo, "checkout", "--quiet", "main")
	writeIn(t, repo, "own.txt", "the agent's work\n")
	runGitIn(t, repo, "add", "own.txt")
	runGitIn(t, repo, "commit", "--quiet", "-m", "the agent's work, squashed")
	writeIn(t, repo, "elsewhere.txt", "another pull request\n")
	runGitIn(t, repo, "add", "elsewhere.txt")
	runGitIn(t, repo, "commit", "--quiet", "-m", "another pull request")
	runGitIn(t, repo, "push", "--quiet")
	runGitIn(t, repo, "checkout", "--quiet", branch)
}

// TestRebaseFastForwardsABranchWhoseWorkLanded is the end-to-end anchor for the move: a branch
// whose commits the base already holds as one squashed commit reaches the base without replaying
// anything, which replaying would turn into a collision with its own content.
func TestRebaseFastForwardsABranchWhoseWorkLanded(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: t.TempDir()})
	repo := initGitRepo(t)

	agentID, tree := rebaseAgent(t, instance, repo)
	landOnTheBase(t, repo)

	refreshed, err := instance.Agents.RebaseWorktree(t.Context(), connect.NewRequest(&agentv1.RebaseWorktreeRequest{
		AgentId: agentID,
	}))
	require.NoError(t, err)

	assert.True(t, refreshed.Msg.GetMoved(), "the branch reached the base without replaying anything")
	assert.False(t, refreshed.Msg.GetConflicted(), "which is the collision the cutoff exists to avoid")
	assert.Zero(t, refreshed.Msg.GetReplayCommits(), "nothing was written after it landed")
	assert.Positive(t, refreshed.Msg.GetLandedCommits(), "and its own work is what left the branch")

	assert.Equal(t, readGitOutput(t, repo, "rev-parse", "origin/main"),
		readGitOutput(t, tree, "rev-parse", "HEAD"), "the branch stands on the base")
}

// TestRebaseParksTheCheckoutBeforeItMoves is the end-to-end anchor for the undo: a rebase that
// drops commits on purpose writes the checkout to a ref of its own first, holding what was never
// committed as well.
func TestRebaseParksTheCheckoutBeforeItMoves(t *testing.T) {
	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: t.TempDir()})
	repo := initGitRepo(t)

	agentID, tree := rebaseAgent(t, instance, repo)
	landOnTheBase(t, repo)

	writeIn(t, tree, "uncommitted.txt", "never committed\n")
	before := readGitOutput(t, tree, "rev-parse", "HEAD")

	refreshed, err := instance.Agents.RebaseWorktree(t.Context(), connect.NewRequest(&agentv1.RebaseWorktreeRequest{
		AgentId: agentID,
	}))
	require.NoError(t, err)
	require.True(t, refreshed.Msg.GetMoved(), "the fixture did not drop the commits this is about")
	require.Zero(t, refreshed.Msg.GetReplayCommits())

	parked := readGitOutput(t, tree, "for-each-ref", "--format=%(refname)", "refs/weave/prerefresh/"+agentID+"/")
	require.NotEmpty(t, parked, "the undo is written before anything moves")

	assert.Equal(t, before, readGitOutput(t, tree, "rev-parse", parked+"^"),
		"the park stands on the commit the branch was at")
	assert.Equal(t, "never committed", readGitOutput(t, tree, "show", parked+":uncommitted.txt"),
		"and holds what was never committed, which the reflog could not")
}
