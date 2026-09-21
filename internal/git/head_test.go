//go:build integration

package git_test

import (
	"testing"

	"weave/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadHeadStateReadsASettledCheckout covers the ordinary answer.
func TestReadHeadStateReadsASettledCheckout(t *testing.T) {
	t.Parallel()

	dir := newRepo(t)

	head := client.ReadHeadState(t.Context(), dir)

	assert.Equal(t, "main", head.Branch)
	assert.False(t, head.SequenceInProgress)
	assert.Empty(t, head.RepositoryURL)
}

// TestReadHeadStateReadsTheRepository covers the identity a checkout carries: the canonical
// address of its origin, and nothing for an origin that names no forge.
func TestReadHeadStateReadsTheRepository(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		origin string
		want   string
	}{
		{
			name:   "an scp-like origin reads as its canonical address",
			origin: "git@github.com:harrisoncramer/weave.git",
			want:   "https://github.com/harrisoncramer/weave",
		},
		{
			name:   "an https origin reads as its canonical address",
			origin: "https://github.com/harrisoncramer/weave.git",
			want:   "https://github.com/harrisoncramer/weave",
		},
		{
			name:   "a local origin reads as no repository",
			origin: "/tmp/somewhere/else.git",
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := newRepo(t)
			run(t, dir, "remote", "add", "origin", tt.origin)

			head := client.ReadHeadState(t.Context(), dir)

			assert.Equal(t, "main", head.Branch)
			assert.Equal(t, tt.want, head.RepositoryURL)
		})
	}
}

// TestReadHeadStateNamesTheBranchAStoppedRebaseBelongsTo covers the case the row draws from,
// where git has detached HEAD and the branch is only recoverable from the stopped sequence.
func TestReadHeadStateNamesTheBranchAStoppedRebaseBelongsTo(t *testing.T) {
	t.Parallel()

	dir := stoppedRebase(t)

	branch, err := git.Run(t.Context(), dir, "rev-parse", "--abbrev-ref", "HEAD")
	require.NoError(t, err)
	require.Equal(t, "HEAD", branch)

	head := client.ReadHeadState(t.Context(), dir)

	assert.Equal(t, "topic", head.Branch)
	assert.True(t, head.SequenceInProgress)
}

// TestReadHeadStateCarriesNoBranchForARebaseOffADetachedHead covers what git writes into head-name
// when the rebase started from no branch.
func TestReadHeadStateCarriesNoBranchForARebaseOffADetachedHead(t *testing.T) {
	t.Parallel()

	dir := stoppedRebase(t, "--detach")

	head := client.ReadHeadState(t.Context(), dir)

	assert.Empty(t, head.Branch)
	assert.True(t, head.SequenceInProgress)
}

// TestReadHeadStateReadsAStoppedMerge covers a sequence that holds the index without detaching.
func TestReadHeadStateReadsAStoppedMerge(t *testing.T) {
	t.Parallel()

	dir := newRepo(t)
	conflict(t, dir)

	head := client.ReadHeadState(t.Context(), dir)

	assert.Equal(t, "main", head.Branch)
	assert.True(t, head.SequenceInProgress)
}

// TestReadHeadStateClearsItselfWhenTheRebaseIsSettled covers the recovery nobody tells weave about.
func TestReadHeadStateClearsItselfWhenTheRebaseIsSettled(t *testing.T) {
	t.Parallel()

	dir := stoppedRebase(t)
	run(t, dir, "rebase", "--abort")

	head := client.ReadHeadState(t.Context(), dir)

	assert.Equal(t, "topic", head.Branch)
	assert.False(t, head.SequenceInProgress)
}

// TestReadHeadStateAnswersEmptyForSomewhereGitWillNotSpeakFor covers a directory that is no
// checkout, which reads as no branch rather than as a failure.
func TestReadHeadStateAnswersEmptyForSomewhereGitWillNotSpeakFor(t *testing.T) {
	t.Parallel()

	head := client.ReadHeadState(t.Context(), t.TempDir())

	assert.Empty(t, head.Branch)
	assert.False(t, head.SequenceInProgress)
}

// stoppedRebase leaves a checkout part-way through a rebase of topic that it cannot settle,
// taking the flags topic is checked out with.
func stoppedRebase(t *testing.T, checkout ...string) string {
	t.Helper()

	dir := newRepo(t)
	run(t, dir, "checkout", "--quiet", "-b", "topic")
	writeFile(t, dir, "tracked.txt", "topic")
	run(t, dir, "commit", "--quiet", "-am", "topic")
	run(t, dir, "checkout", "--quiet", "main")
	writeFile(t, dir, "tracked.txt", "moved on")
	run(t, dir, "commit", "--quiet", "-am", "moved on")
	run(t, dir, append([]string{"checkout", "--quiet"}, append(checkout, "topic")...)...)

	// Expected to fail.
	_, _ = git.Run(t.Context(), dir, "rebase", "main")

	return dir
}
