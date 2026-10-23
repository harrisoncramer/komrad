//go:build integration

package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/git"
	taskv1 "komrad/proto/gen/task/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationAbortSequence undoes each kind of sequence git can leave part-way through.
func TestIntegrationAbortSequence(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name     string
		build    func(t *testing.T) string
		sequence taskv1.Sequence
		lock     bool
		wantErr  error
	}

	tests := []testCase{
		{
			name: "a checkout another git command holds is left alone",
			build: func(t *testing.T) string {
				t.Helper()

				return stoppedRebase(t)
			},
			sequence: taskv1.Sequence_SEQUENCE_REBASE,
			lock:     true,
			wantErr:  git.ErrIndexLocked,
		},
		{
			name: "a stopped rebase",
			build: func(t *testing.T) string {
				t.Helper()

				return stoppedRebase(t)
			},
			sequence: taskv1.Sequence_SEQUENCE_REBASE,
		},
		{
			name:     "a rebase replaying a merge",
			build:    stoppedRebaseOfAMerge,
			sequence: taskv1.Sequence_SEQUENCE_REBASE,
		},
		{
			name:     "a stopped am",
			build:    stoppedApply,
			sequence: taskv1.Sequence_SEQUENCE_AM,
		},
		{
			name: "a stopped merge",
			build: func(t *testing.T) string {
				t.Helper()

				dir := newRepo(t)
				conflict(t, dir)

				return dir
			},
			sequence: taskv1.Sequence_SEQUENCE_MERGE,
		},
		{
			name: "a multi-commit cherry-pick between its picks",
			build: func(t *testing.T) string {
				t.Helper()

				return restingPicks(t, "cherry-pick")
			},
			sequence: taskv1.Sequence_SEQUENCE_CHERRY_PICK,
		},
		{
			name: "a multi-commit revert between its picks",
			build: func(t *testing.T) string {
				t.Helper()

				return restingPicks(t, "revert")
			},
			sequence: taskv1.Sequence_SEQUENCE_REVERT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := tt.build(t)
			client := git.NewClient(git.NewClientParams{})
			if tt.lock {
				gitDir := out(t, dir, "rev-parse", "--absolute-git-dir")
				require.NoError(t, os.WriteFile(filepath.Join(gitDir, "index.lock"), nil, 0o600))
			}

			err := client.AbortSequence(t.Context(), dir, tt.sequence)

			left, readErr := client.ReadSequence(t.Context(), dir)
			require.NoError(t, readErr)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, tt.sequence, left)

				return
			}
			require.NoError(t, err)
			assert.Equal(t, taskv1.Sequence_SEQUENCE_NONE, left)
		})
	}
}

// TestIntegrationReadRebaseOnto reads the commit a stopped rebase is replaying onto.
func TestIntegrationReadRebaseOnto(t *testing.T) {
	t.Parallel()

	client := git.NewClient(git.NewClientParams{})

	idle, err := client.ReadRebaseOnto(t.Context(), newRepo(t))
	require.NoError(t, err)
	assert.Empty(t, idle)

	dir := stoppedRebase(t)
	onto, err := client.ReadRebaseOnto(t.Context(), dir)
	require.NoError(t, err)
	assert.Equal(t, out(t, dir, "rev-parse", "main"), onto)
}
