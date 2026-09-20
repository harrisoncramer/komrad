package worktree

import (
	"testing"
	"time"

	"weave/internal/dbtest"
	"weave/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func open(t *testing.T) *Store {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return New(pool.GetDB())
}

// TestUpsertReplacesAMeasurement covers a second write over the same directory replacing the
// first, an error clearing the bytes and a later success clearing the error.
func TestUpsertReplacesAMeasurement(t *testing.T) {
	sizes := open(t)
	first := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

	require.NoError(t, sizes.Upsert(t.Context(), Size{
		Dir:        "/trees/a",
		Bytes:      42,
		MeasuredAt: first,
	}))
	require.NoError(t, sizes.Upsert(t.Context(), Size{
		Dir:        "/trees/a",
		MeasuredAt: first.Add(time.Hour),
		Error:      "permission denied",
	}))

	listed, err := sizes.ListSizes(t.Context())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "permission denied", listed[0].Error)
	assert.Zero(t, listed[0].Bytes)
	assert.True(t, listed[0].MeasuredAt.Equal(first.Add(time.Hour)))

	require.NoError(t, sizes.Upsert(t.Context(), Size{
		Dir:        "/trees/a",
		Bytes:      7,
		MeasuredAt: first.Add(2 * time.Hour),
	}))

	listed, err = sizes.ListSizes(t.Context())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Empty(t, listed[0].Error)
	assert.Equal(t, int64(7), listed[0].Bytes)
	assert.True(t, listed[0].MeasuredAt.Equal(first.Add(2*time.Hour)))
}

// TestDeleteForgetsOnlyTheDirectoriesNamed covers the rows named going and the rest staying,
// with nothing named touching nothing.
func TestDeleteForgetsOnlyTheDirectoriesNamed(t *testing.T) {
	sizes := open(t)
	at := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	for _, dir := range []string{"/trees/a", "/trees/b", "/trees/c"} {
		require.NoError(t, sizes.Upsert(t.Context(), Size{
			Dir:        dir,
			Bytes:      1,
			MeasuredAt: at,
		}))
	}

	require.NoError(t, sizes.Delete(t.Context(), nil))
	require.NoError(t, sizes.Delete(t.Context(), []string{"/trees/a", "/trees/c", "/trees/never"}))

	listed, err := sizes.ListSizes(t.Context())
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, "/trees/b", listed[0].Dir)
}
