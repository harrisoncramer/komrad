//go:build integration

package git_test

import (
	"path/filepath"
	"testing"

	"komrad/internal/git"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationReadSequence reads each kind of sequence git can leave part-way through.
func TestIntegrationReadSequence(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name  string
		build func(t *testing.T) string
		want  git.Sequence
	}

	tests := []testCase{
		{
			name:  "a rebase replaying a merge reads as the rebase rather than the merge it wrote",
			build: stoppedRebaseOfAMerge,
			want:  git.SequenceRebase,
		},
		{
			name:  "a rebase on the apply backend reads as a rebase",
			build: stoppedApplyRebase,
			want:  git.SequenceRebase,
		},
		{
			name:  "a stopped am reads as an am",
			build: stoppedApply,
			want:  git.SequenceApply,
		},
		{
			name: "a multi-commit cherry-pick between its picks reads as a cherry-pick",
			build: func(t *testing.T) string {
				t.Helper()

				return restingPicks(t, "cherry-pick")
			},
			want: git.SequenceCherryPick,
		},
		{
			name: "a multi-commit revert between its picks reads as a revert",
			build: func(t *testing.T) string {
				t.Helper()

				return restingPicks(t, "revert")
			},
			want: git.SequenceRevert,
		},
		{
			name: "a stopped merge reads as a merge",
			build: func(t *testing.T) string {
				t.Helper()

				dir := newRepo(t)
				conflict(t, dir)

				return dir
			},
			want: git.SequenceMerge,
		},
		{
			name: "a stopped rebase reads as a rebase",
			build: func(t *testing.T) string {
				t.Helper()

				return stoppedRebase(t)
			},
			want: git.SequenceRebase,
		},
		{
			name:  "a settled checkout has nothing in progress",
			build: newRepo,
			want:  git.SequenceNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := tt.build(t)

			got, err := client.ReadSequence(t.Context(), dir)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// stoppedRebaseOfAMerge leaves a rebase --rebase-merges stopped on the merge it could not
// recreate, which is the one place git writes MERGE_HEAD inside a rebase.
func stoppedRebaseOfAMerge(t *testing.T) string {
	t.Helper()

	dir := newRepo(t)
	run(t, dir, "checkout", "--quiet", "-b", "side")
	commitFile(t, dir, "clash.txt", "side\n", "side")
	run(t, dir, "checkout", "--quiet", "-b", "topic", "main")
	commitFile(t, dir, "clash.txt", "topic\n", "topic")

	// Expected to conflict.
	_, _ = git.Run(t.Context(), dir, "merge", "--no-edit", "side")
	commitFile(t, dir, "clash.txt", "settled\n", "merge side")

	run(t, dir, "checkout", "--quiet", "main")
	commitFile(t, dir, "other.txt", "moved on\n", "moved on")
	run(t, dir, "checkout", "--quiet", "topic")

	// Expected to stop on the merge.
	_, _ = git.Run(t.Context(), dir, "rebase", "--rebase-merges", "main")

	require.FileExists(t, gitPath(t, dir, "MERGE_HEAD"), "failed to stop the rebase on its merge")

	return dir
}

// stoppedApply leaves git am stopped on a patch that no longer applies.
func stoppedApply(t *testing.T) string {
	t.Helper()

	dir := newRepo(t)
	commitFile(t, dir, "tracked.txt", "patched\n", "patched")
	patch := out(t, dir, "format-patch", "-1", "--stdout")
	run(t, dir, "reset", "--quiet", "--hard", "HEAD~1")
	commitFile(t, dir, "tracked.txt", "moved on\n", "moved on")
	writeFile(t, dir, "change.patch", patch+"\n")

	// Expected to fail.
	_, _ = git.Run(t.Context(), dir, "am", "change.patch")

	return dir
}

// restingPicks leaves a two-commit cherry-pick or revert resting after its first pick was settled
// by hand, when only the sequencer directory says it is unfinished.
func restingPicks(t *testing.T, verb string) string {
	t.Helper()

	dir := newRepo(t)
	run(t, dir, "checkout", "--quiet", "-b", "picks")
	commitFile(t, dir, "tracked.txt", "first\n", "first")
	commitFile(t, dir, "tracked.txt", "second\n", "second")

	args := []string{"cherry-pick", "picks~2..picks"}
	if verb == "revert" {
		commitFile(t, dir, "tracked.txt", "third\n", "third")
		args = []string{"revert", "--no-edit", "HEAD~1", "HEAD~2"}
	} else {
		run(t, dir, "checkout", "--quiet", "main")
		commitFile(t, dir, "tracked.txt", "moved on\n", "moved on")
	}

	// Expected to stop on the first pick.
	_, _ = git.Run(t.Context(), dir, args...)
	commitFile(t, dir, "tracked.txt", "settled\n", "settled")

	require.DirExists(t, gitPath(t, dir, "sequencer"), "failed to leave the %s resting between picks", verb)

	return dir
}

func gitPath(t *testing.T, dir, name string) string {
	t.Helper()

	return filepath.Join(out(t, dir, "rev-parse", "--absolute-git-dir"), name)
}

// stoppedApplyRebase leaves a rebase on the apply backend stopped, which keeps its state where am
// keeps its own.
func stoppedApplyRebase(t *testing.T) string {
	t.Helper()

	dir := newRepo(t)
	run(t, dir, "checkout", "--quiet", "-b", "topic")
	commitFile(t, dir, "tracked.txt", "topic\n", "topic")
	run(t, dir, "checkout", "--quiet", "main")
	commitFile(t, dir, "tracked.txt", "moved on\n", "moved on")
	run(t, dir, "checkout", "--quiet", "topic")

	// Expected to fail.
	_, _ = git.Run(t.Context(), dir, "rebase", "--apply", "main")

	require.DirExists(t, gitPath(t, dir, "rebase-apply"), "failed to stop the rebase on the apply backend")

	return dir
}
