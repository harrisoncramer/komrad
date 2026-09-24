package worktree

import (
	"testing"
	"time"

	"weave/internal/dbtest"
	"weave/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpsertLeavesTheSizeAlone covers a second read of a directory replacing what the filesystem
// said of it while keeping the measurement the size loop wrote.
func TestUpsertLeavesTheSizeAlone(t *testing.T) {
	worktrees := openWorktreeStore(t)
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	require.NoError(t, worktrees.Upsert(t.Context(), Worktree{
		Dir:        "/trees/a",
		RepoDir:    "/repo",
		Branch:     "agent/a",
		Readable:   true,
		ModifiedAt: &at,
	}))
	require.NoError(t, worktrees.RecordSize(t.Context(), Size{
		Dir:        "/trees/a",
		Bytes:      42,
		MeasuredAt: at,
	}))
	require.NoError(t, worktrees.Upsert(t.Context(), Worktree{
		Dir:     "/trees/a",
		RepoDir: "/repo",
	}))

	listed, err := worktrees.List(t.Context())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Empty(t, listed[0].Branch)
	assert.False(t, listed[0].Readable)
	assert.Nil(t, listed[0].ModifiedAt)
	require.NotNil(t, listed[0].Size)
	assert.Equal(t, int64(42), listed[0].Size.Bytes)
}

// TestRecordSizeReplacesAMeasurement covers an error clearing the bytes, a later success
// clearing the error, and a directory with no row taking no measurement.
func TestRecordSizeReplacesAMeasurement(t *testing.T) {
	worktrees := openWorktreeStore(t)
	first := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	require.NoError(t, worktrees.Upsert(t.Context(), Worktree{Dir: "/trees/a"}))

	listed, err := worktrees.List(t.Context())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Nil(t, listed[0].Size, "a directory is unmeasured until its first walk")

	require.NoError(t, worktrees.RecordSize(t.Context(), Size{
		Dir:        "/trees/a",
		MeasuredAt: first,
		Error:      "permission denied",
	}))

	sizes, err := worktrees.ListSizes(t.Context())
	require.NoError(t, err)
	require.Len(t, sizes, 1)
	assert.Equal(t, "permission denied", sizes[0].Error)
	assert.Zero(t, sizes[0].Bytes)
	assert.True(t, sizes[0].MeasuredAt.Equal(first))

	require.NoError(t, worktrees.RecordSize(t.Context(), Size{
		Dir:        "/trees/a",
		Bytes:      7,
		MeasuredAt: first.Add(time.Hour),
	}))
	require.NoError(t, worktrees.RecordSize(t.Context(), Size{
		Dir:        "/trees/never",
		Bytes:      9,
		MeasuredAt: first,
	}))

	sizes, err = worktrees.ListSizes(t.Context())
	require.NoError(t, err)
	require.Len(t, sizes, 1)
	assert.Empty(t, sizes[0].Error)
	assert.Equal(t, int64(7), sizes[0].Bytes)
	assert.True(t, sizes[0].MeasuredAt.Equal(first.Add(time.Hour)))
}

// TestDeleteForgetsOnlyTheDirectoriesNamed covers the rows named going and the rest staying,
// with nothing named touching nothing.
func TestDeleteForgetsOnlyTheDirectoriesNamed(t *testing.T) {
	worktrees := openWorktreeStore(t)
	for _, dir := range []string{"/trees/a", "/trees/b", "/trees/c"} {
		require.NoError(t, worktrees.Upsert(t.Context(), Worktree{Dir: dir}))
	}

	require.NoError(t, worktrees.Delete(t.Context(), nil))
	require.NoError(t, worktrees.Delete(t.Context(), []string{"/trees/a", "/trees/c", "/trees/never"}))

	listed, err := worktrees.List(t.Context())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "/trees/b", listed[0].Dir)
}

// TestListWatchedReadsTheKeysNamed covers nil keys reading every row and a list reading only
// the directories it names.
func TestListWatchedReadsTheKeysNamed(t *testing.T) {
	worktrees := openWorktreeStore(t)
	for _, dir := range []string{"/trees/a", "/trees/b"} {
		require.NoError(t, worktrees.Upsert(t.Context(), Worktree{Dir: dir}))
	}

	all, err := worktrees.ListWatched(t.Context(), nil)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	named, err := worktrees.ListWatched(t.Context(), []string{"/trees/b", "/trees/gone"})
	require.NoError(t, err)
	require.Len(t, named, 1)
	assert.Equal(t, "/trees/b", named[0].Dir)

	none, err := worktrees.ListWatched(t.Context(), []string{})
	require.NoError(t, err)
	assert.Empty(t, none)
}

func openWorktreeStore(t *testing.T) *WorktreeStore {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return NewWorktreeStore(pool.GetDB())
}
