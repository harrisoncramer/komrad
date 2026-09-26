//go:build integration

package git_test

import (
	"testing"

	"komrad/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// movedBase is two rounds of one agent's work either side of the base moving on: the first
// snapshot on the old base, and the second on a base that changed the same file above the
// agent's lines and another file the agent never touched.
type movedBase struct {
	oldBase, oldSnapshot, newBase, newSnapshot string
}

func newMovedBase(t *testing.T) (string, movedBase) {
	t.Helper()

	repo := newRepo(t)

	var fixture movedBase

	writeFile(t, repo, "greet.go", "package greet\n\nfunc Hello() {}\n")
	run(t, repo, "add", "greet.go")
	run(t, repo, "commit", "--quiet", "-m", "base")
	fixture.oldBase = out(t, repo, "rev-parse", "HEAD")

	run(t, repo, "checkout", "--quiet", "-b", "agent")
	fixture.oldSnapshot = commitFile(t, repo, "greet.go",
		"package greet\n\nfunc Hello() {}\n\nfunc Agent() {}\n", "round one")

	run(t, repo, "checkout", "--quiet", "main")
	writeFile(t, repo, "greet.go", "// main moved\npackage greet\n\nfunc Hello() {}\n")
	writeFile(t, repo, "other.go", "package greet\n")
	run(t, repo, "add", "greet.go", "other.go")
	run(t, repo, "commit", "--quiet", "-m", "main moved")
	fixture.newBase = out(t, repo, "rev-parse", "HEAD")

	run(t, repo, "checkout", "--quiet", "agent")
	run(t, repo, "rebase", "--quiet", "main")
	fixture.newSnapshot = commitFile(t, repo, "greet.go",
		"// main moved\npackage greet\n\nfunc Hello() {}\n\nfunc Agent() {}\n\nfunc Again() {}\n",
		"round two")

	return repo, fixture
}

// TestIntegrationReplayCarriesOnlyTheAgentsOwnChange is the case this exists for: two rounds either side
// of a moved base compare as the agent's own work, with the base's movement gone.
func TestIntegrationReplayCarriesOnlyTheAgentsOwnChange(t *testing.T) {
	repo, fixture := newMovedBase(t)

	// The naive comparison carries main's own change as though the agent wrote it.
	naive := out(t, repo, "diff", "--name-only", fixture.oldSnapshot, fixture.newSnapshot)
	require.Contains(t, naive, "other.go", "the fixture has to smear the base's movement first")

	replayed, err := client.Replay(t.Context(), git.ReplayParams{
		Dir:         repo,
		FromBase:    fixture.oldBase,
		OntoBase:    fixture.newBase,
		SnapshotSHA: fixture.oldSnapshot,
	})
	require.NoError(t, err)
	require.Empty(t, replayed.Conflicts)

	changed := out(t, repo, "diff", "--name-only", replayed.Tree, fixture.newSnapshot)
	assert.Equal(t, "greet.go", changed, "only the file the agent touched between the rounds")

	diff := out(t, repo, "diff", "--unified=0", replayed.Tree, fixture.newSnapshot)
	assert.Contains(t, diff, "+func Again() {}")
	assert.NotContains(t, diff, "main moved", "the base's own line is on both sides")
}

// TestIntegrationReplayIsTheSameTreeTwice pins that a chapter drawn twice reads its old side from one tree.
func TestIntegrationReplayIsTheSameTreeTwice(t *testing.T) {
	repo, fixture := newMovedBase(t)

	params := git.ReplayParams{
		Dir:         repo,
		FromBase:    fixture.oldBase,
		OntoBase:    fixture.newBase,
		SnapshotSHA: fixture.oldSnapshot,
	}

	first, err := client.Replay(t.Context(), params)
	require.NoError(t, err)

	second, err := client.Replay(t.Context(), params)
	require.NoError(t, err)

	assert.Equal(t, first.Tree, second.Tree)
}

// TestIntegrationReplayNamesThePathsTheBaseRewroteUnderTheAgent covers the one case no honest comparison
// exists for, where the base and the agent rewrote the same lines.
func TestIntegrationReplayNamesThePathsTheBaseRewroteUnderTheAgent(t *testing.T) {
	repo, fixture := newMovedBase(t)

	run(t, repo, "checkout", "--quiet", "--detach", fixture.oldBase)
	clashing := commitFile(t, repo, "greet.go",
		"package greet\n\nfunc Hello() {}\n\nfunc Main() {}\n", "main rewrote the same lines")

	replayed, err := client.Replay(t.Context(), git.ReplayParams{
		Dir:         repo,
		FromBase:    fixture.oldBase,
		OntoBase:    clashing,
		SnapshotSHA: fixture.oldSnapshot,
	})
	require.NoError(t, err)

	assert.Equal(t, []string{"greet.go"}, replayed.Conflicts)
	assert.NotEmpty(t, replayed.Tree, "the tree is still written, with the markers in it")
}

// TestIntegrationReplayRefusesAMissingCommit covers a caller handing over less than the three commits a
// replay is made of.
func TestIntegrationReplayRefusesAMissingCommit(t *testing.T) {
	repo, fixture := newMovedBase(t)

	_, err := client.Replay(t.Context(), git.ReplayParams{
		Dir:         repo,
		FromBase:    fixture.oldBase,
		SnapshotSHA: fixture.oldSnapshot,
	})
	require.Error(t, err)

	_, err = client.Replay(t.Context(), git.ReplayParams{
		Dir:         repo,
		FromBase:    fixture.oldBase,
		OntoBase:    "0000000000000000000000000000000000000000",
		SnapshotSHA: fixture.oldSnapshot,
	})
	require.Error(t, err, "a commit no repository holds is a failure rather than an answer")
}

// TestIntegrationIsAncestorTellsAMovedBaseFromARewrittenOne covers the two ways a base stops matching.
func TestIntegrationIsAncestorTellsAMovedBaseFromARewrittenOne(t *testing.T) {
	repo, fixture := newMovedBase(t)

	forward, err := client.IsAncestor(t.Context(), git.IsAncestorParams{
		Dir:           repo,
		MaybeAncestor: fixture.oldBase,
		Of:            fixture.newBase,
	})
	require.NoError(t, err)
	assert.True(t, forward, "the base moved on")

	rewritten, err := client.IsAncestor(t.Context(), git.IsAncestorParams{
		Dir:           repo,
		MaybeAncestor: fixture.newBase,
		Of:            fixture.oldBase,
	})
	require.NoError(t, err)
	assert.False(t, rewritten, "the old base is not reachable from the new one")
}
