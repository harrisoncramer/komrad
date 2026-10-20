//go:build integration

package e2e

import (
	"strings"
	"testing"

	"komrad/internal/diff"
	"komrad/internal/git"
	"komrad/internal/komradtest"
	agentv1 "komrad/proto/gen/agent/v1"
	workflowv1 "komrad/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	greetBefore    = "package greet\n\nfunc Hello() {}\n"
	greetSnapshot1 = "package greet\n\nfunc Hello() {}\n\nfunc Agent() {}\n"
)

// snapshotsAcrossARebase is two snapshots of one agent taken through the server either side of the
// rebase key, with a review released on the first.
type snapshotsAcrossARebase struct {
	tree          string
	first, second *agentv1.ReviewSnapshot
}

// reviewAcrossARebase takes snapshot one, releases a review on it, lands mainsGreet on main under
// the agent, rebases through the server, and takes snapshot two of the agent's next edit.
func reviewAcrossARebase(t *testing.T, mainsGreet string, wantConflict bool) snapshotsAcrossARebase {
	t.Helper()

	isolateGit(t)

	instance := komradtest.Start(t, komradtest.Params{WorktreesDir: t.TempDir()})
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
	taskID := created.Msg.GetAgent().GetTaskId()
	require.DirExists(t, tree)
	instance.WaitForHarness(agentID)
	runGitIn(t, tree, "config", "user.email", "test@komrad.test")
	runGitIn(t, tree, "config", "user.name", "komrad test")

	writeIn(t, tree, "greet.go", greetSnapshot1)
	runGitIn(t, tree, "add", "greet.go")
	runGitIn(t, tree, "commit", "--quiet", "-m", "snapshot one")

	first, err := instance.Agents.TakeReviewSnapshot(ctx, connect.NewRequest(&agentv1.TakeReviewSnapshotRequest{
		Task: &taskID,
	}))
	require.NoError(t, err)

	_, err = instance.Agents.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
		Task:        &taskID,
		FilePath:    "greet.go",
		NewStart:    5,
		NewEnd:      5,
		QuotedLines: []string{"+func Agent() {}"},
		Body:        "name this",
	}))
	require.NoError(t, err)

	_, err = instance.Agents.ReleaseReview(ctx, connect.NewRequest(&agentv1.ReleaseReviewRequest{
		Task: &taskID,
	}))
	require.NoError(t, err)

	runGitIn(t, repo, "checkout", "--quiet", "main")
	writeIn(t, repo, "greet.go", mainsGreet)
	writeIn(t, repo, "other.go", "package greet\n")
	runGitIn(t, repo, "add", "greet.go", "other.go")
	runGitIn(t, repo, "commit", "--quiet", "-m", "another pull request")
	runGitIn(t, repo, "push", "--quiet")

	rebased, err := instance.Agents.RebaseWorktree(ctx, connect.NewRequest(&agentv1.RebaseWorktreeRequest{
		Task: taskID,
	}))
	require.NoError(t, err)

	require.Equal(t, wantConflict, rebased.Msg.GetConflicted(), "the fixture has to take the path its test names")

	if wantConflict {
		writeIn(t, tree, "greet.go", strings.Replace(mainsGreet, "func Hello() {}", "func Hello() {}\n\nfunc Agent() {}", 1))
		runGitIn(t, tree, "add", "greet.go")
		runGitIn(t, tree, "-c", "core.editor=true", "rebase", "--continue")
	}

	require.Equal(t, readGitOutput(t, repo, "rev-parse", "origin/main"),
		readGitOutput(t, tree, "merge-base", "origin/main", "HEAD"),
		"the fixture has to leave the branch on the moved base")

	rebasedGreet := readGitOutput(t, tree, "show", "HEAD:greet.go") + "\n"
	writeIn(t, tree, "greet.go", strings.Replace(rebasedGreet, "func Agent() {}", "func Agent() {}\n\nfunc Again() {}", 1))

	second, err := instance.Agents.TakeReviewSnapshot(ctx, connect.NewRequest(&agentv1.TakeReviewSnapshotRequest{
		Task: &taskID,
	}))
	require.NoError(t, err)
	require.NotEqual(t, first.Msg.GetSnapshot().GetBaseSha(), second.Msg.GetSnapshot().GetBaseSha(),
		"the fixture has to move the base between the two snapshots")

	return snapshotsAcrossARebase{
		tree:   tree,
		first:  first.Msg.GetSnapshot(),
		second: second.Msg.GetSnapshot(),
	}
}

// TestIntegrationSnapshotsEitherSideOfARebaseReplayToTheAgentsOwnChange takes two snapshots through the server
// and the rebase key and proves what the review screen draws between them: the first snapshot
// carried onto the moved base, diffed against the second, is the agent's own edit and nothing
// the base brought with it.
func TestIntegrationSnapshotsEitherSideOfARebaseReplayToTheAgentsOwnChange(t *testing.T) {
	snapshots := reviewAcrossARebase(t, "// main moved\n"+greetBefore, false)
	client := git.NewClient(git.NewClientParams{})

	naive := readGitOutput(t, snapshots.tree, "diff", "--name-only",
		snapshots.first.GetSha(), snapshots.second.GetSha())
	require.Contains(t, naive, "other.go", "the two snapshots' own diff carries the base's move")

	forward, err := client.IsAncestor(t.Context(), git.IsAncestorParams{
		Dir:           snapshots.tree,
		MaybeAncestor: snapshots.first.GetBaseSha(),
		Of:            snapshots.second.GetBaseSha(),
	})
	require.NoError(t, err)
	assert.True(t, forward, "the rebase key moves the base forward rather than rewriting it")

	replayed, err := client.Replay(t.Context(), git.ReplayParams{
		Dir:         snapshots.tree,
		FromBase:    snapshots.first.GetBaseSha(),
		OntoBase:    snapshots.second.GetBaseSha(),
		SnapshotSHA: snapshots.first.GetSha(),
	})
	require.NoError(t, err)
	require.Empty(t, replayed.Conflicts)

	patch, err := diff.Load(t.Context(), client, diff.LoadParams{
		Dir:         snapshots.tree,
		BaseSHA:     replayed.Tree,
		SnapshotSHA: snapshots.second.GetSha(),
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

// TestIntegrationSnapshotsEitherSideOfAConflictedRebaseNameThePath covers the agent resolving a real clash during
// the rebase.
func TestIntegrationSnapshotsEitherSideOfAConflictedRebaseNameThePath(t *testing.T) {
	snapshots := reviewAcrossARebase(t, "package greet\n\nfunc Hello() {}\n\nfunc Main() {}\n", true)

	replayed, err := git.NewClient(git.NewClientParams{}).Replay(t.Context(), git.ReplayParams{
		Dir:         snapshots.tree,
		FromBase:    snapshots.first.GetBaseSha(),
		OntoBase:    snapshots.second.GetBaseSha(),
		SnapshotSHA: snapshots.first.GetSha(),
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"greet.go"}, replayed.Conflicts)
}
