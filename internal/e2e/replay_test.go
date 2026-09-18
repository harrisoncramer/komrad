//go:build integration

package e2e

import (
	"strings"
	"testing"

	"weave/internal/diff"
	"weave/internal/git"
	"weave/internal/weavetest"
	agentv1 "weave/proto/gen/agent/v1"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	greetBefore = "package greet\n\nfunc Hello() {}\n"
	greetRound1 = "package greet\n\nfunc Hello() {}\n\nfunc Agent() {}\n"
)

// roundsAcrossARebase is two rounds of one agent taken through the service either side of the
// rebase key, with a review released on the first.
type roundsAcrossARebase struct {
	tree          string
	first, second *agentv1.ReviewRound
}

// reviewAcrossARebase takes round one, releases a review on it, lands mainsGreet on main under
// the agent, rebases through the service, and takes round two of the agent's next edit.
func reviewAcrossARebase(t *testing.T, mainsGreet string) roundsAcrossARebase {
	t.Helper()

	hermeticGit(t)

	instance := weavetest.Start(t, weavetest.Params{WorktreesDir: t.TempDir()})
	repo := initGitRepo(t)
	ctx := t.Context()

	writeIn(t, repo, "greet.go", greetBefore)
	runGitIn(t, repo, "add", "greet.go")
	runGitIn(t, repo, "commit", "--quiet", "-m", "greeting")
	runGitIn(t, repo, "push", "--quiet")

	workflow, err := instance.Workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:     "backend",
		Cwd:      repo,
		Worktree: &workflowv1.WorktreeConfig{Enabled: true, BaseBranch: "main"},
	}))
	require.NoError(t, err)

	created, err := instance.Agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     "do the work",
		Title:      "auth",
		WorkflowId: workflow.Msg.GetWorkflow().GetId(),
	}))
	require.NoError(t, err)

	agentID, tree := created.Msg.GetAgent().GetId(), created.Msg.GetAgent().GetCwd()
	require.DirExists(t, tree)
	runGitIn(t, tree, "config", "user.email", "test@weave.test")
	runGitIn(t, tree, "config", "user.name", "weave test")

	writeIn(t, tree, "greet.go", greetRound1)
	runGitIn(t, tree, "add", "greet.go")
	runGitIn(t, tree, "commit", "--quiet", "-m", "round one")

	first, err := instance.Agents.OpenReviewRound(ctx, connect.NewRequest(&agentv1.OpenReviewRoundRequest{
		AgentId: agentID,
	}))
	require.NoError(t, err)

	_, err = instance.Agents.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
		AgentId:     agentID,
		FilePath:    "greet.go",
		NewStart:    5,
		NewEnd:      5,
		QuotedLines: []string{"+func Agent() {}"},
		Body:        "name this",
	}))
	require.NoError(t, err)

	_, err = instance.Agents.ReleaseReview(ctx, connect.NewRequest(&agentv1.ReleaseReviewRequest{
		AgentId: agentID,
		Verdict: agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED,
	}))
	require.NoError(t, err)

	// Another pull request lands: a file of its own, and a change to the greeting.
	runGitIn(t, repo, "checkout", "--quiet", "main")
	writeIn(t, repo, "greet.go", mainsGreet)
	writeIn(t, repo, "other.go", "package greet\n")
	runGitIn(t, repo, "add", "greet.go", "other.go")
	runGitIn(t, repo, "commit", "--quiet", "-m", "another pull request")
	runGitIn(t, repo, "push", "--quiet")

	rebased, err := instance.Agents.RebaseWorktree(ctx, connect.NewRequest(&agentv1.RebaseWorktreeRequest{
		AgentId: agentID,
	}))
	require.NoError(t, err)

	if rebased.Msg.GetConflicted() {
		// The agent settles the clash itself, keeping both sides, and carries on.
		writeIn(t, tree, "greet.go", strings.Replace(mainsGreet, "func Hello() {}", "func Hello() {}\n\nfunc Agent() {}", 1))
		runGitIn(t, tree, "add", "greet.go")
		runGitIn(t, tree, "-c", "core.editor=true", "rebase", "--continue")
	}

	require.Equal(t, readGitOutput(t, repo, "rev-parse", "origin/main"),
		readGitOutput(t, tree, "merge-base", "origin/main", "HEAD"),
		"the fixture has to leave the branch on the moved base")

	rebasedGreet := readGitOutput(t, tree, "show", "HEAD:greet.go") + "\n"
	writeIn(t, tree, "greet.go", strings.Replace(rebasedGreet, "func Agent() {}", "func Agent() {}\n\nfunc Again() {}", 1))

	second, err := instance.Agents.OpenReviewRound(ctx, connect.NewRequest(&agentv1.OpenReviewRoundRequest{
		AgentId: agentID,
	}))
	require.NoError(t, err)
	require.NotEqual(t, first.Msg.GetRound().GetBaseSha(), second.Msg.GetRound().GetBaseSha(),
		"the fixture has to move the base between the two rounds")

	return roundsAcrossARebase{
		tree:   tree,
		first:  first.Msg.GetRound(),
		second: second.Msg.GetRound(),
	}
}

// TestRoundsEitherSideOfARebaseReplayToTheAgentsOwnChange takes two rounds through the service
// and the rebase key and proves what the review screen draws between them: the first round
// carried onto the moved base, diffed against the second, is the agent's own edit and nothing
// the base brought with it.
func TestRoundsEitherSideOfARebaseReplayToTheAgentsOwnChange(t *testing.T) {
	rounds := reviewAcrossARebase(t, "// main moved\n"+greetBefore)
	client := git.NewClient()

	naive := readGitOutput(t, rounds.tree, "diff", "--name-only",
		rounds.first.GetSnapshotSha(), rounds.second.GetSnapshotSha())
	require.Contains(t, naive, "other.go", "the two snapshots' own diff carries the base's move")

	forward, err := client.IsAncestor(t.Context(), rounds.tree, rounds.first.GetBaseSha(), rounds.second.GetBaseSha())
	require.NoError(t, err)
	assert.True(t, forward, "the rebase key moves the base forward rather than rewriting it")

	replayed, err := client.Replay(t.Context(), git.ReplayParams{
		Dir:         rounds.tree,
		FromBase:    rounds.first.GetBaseSha(),
		OntoBase:    rounds.second.GetBaseSha(),
		SnapshotSHA: rounds.first.GetSnapshotSha(),
	})
	require.NoError(t, err)
	require.Empty(t, replayed.Conflicts)

	patch, err := diff.Load(t.Context(), client, diff.LoadParams{
		Dir:         rounds.tree,
		BaseSHA:     replayed.Tree,
		SnapshotSHA: rounds.second.GetSnapshotSha(),
	})
	require.NoError(t, err)

	require.Len(t, patch.Files, 1, "only the file the agent edited since the review")
	assert.Equal(t, "greet.go", patch.Files[0].Path())

	var added []string
	for _, hunk := range patch.Files[0].Hunks {
		for _, line := range hunk.Lines {
			if line.Kind == diff.Addition {
				added = append(added, line.Text)
			}
		}
	}

	assert.Equal(t, []string{"", "func Again() {}"}, added, "the agent's own edit, with the base's line on neither side")
}

// TestRoundsEitherSideOfAConflictedRebaseNameThePath covers the agent resolving a real clash
// during the rebase, which is the one case the screen keeps refusing, with the path named.
func TestRoundsEitherSideOfAConflictedRebaseNameThePath(t *testing.T) {
	rounds := reviewAcrossARebase(t, "package greet\n\nfunc Hello() {}\n\nfunc Main() {}\n")

	replayed, err := git.NewClient().Replay(t.Context(), git.ReplayParams{
		Dir:         rounds.tree,
		FromBase:    rounds.first.GetBaseSha(),
		OntoBase:    rounds.second.GetBaseSha(),
		SnapshotSHA: rounds.first.GetSnapshotSha(),
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"greet.go"}, replayed.Conflicts)
}
