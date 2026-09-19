package worktree

import (
	"testing"
	"time"

	"weave/internal/dbtest"
	"weave/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStore(t *testing.T) *Store {
	t.Helper()

	pool, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })

	return New(pool.GetDB())
}

// TestPutReplacesAnEarlierCount covers a second count of the same directory winning, and the
// listing keying by directory.
func TestPutReplacesAnEarlierCount(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	first := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	require.NoError(t, st.Put(ctx, Size{Dir: "/trees/a", Bytes: 10, CountedAt: first}))
	require.NoError(t, st.Put(ctx, Size{Dir: "/trees/b", Bytes: 20, CountedAt: first}))
	require.NoError(t, st.Put(ctx, Size{Dir: "/trees/a", Bytes: 30, CountedAt: first.Add(time.Minute)}))

	sizes, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, sizes, 2)
	assert.Equal(t, int64(30), sizes["/trees/a"].Bytes)
	assert.True(t, sizes["/trees/a"].CountedAt.Equal(first.Add(time.Minute)))
	assert.Equal(t, int64(20), sizes["/trees/b"].Bytes)
}

// TestDeleteDropsOnlyTheNamedDirectories covers the delete leaving the rest alone, and an
// empty delete being nothing.
func TestDeleteDropsOnlyTheNamedDirectories(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()
	at := time.Now()

	require.NoError(t, st.Put(ctx, Size{Dir: "/trees/a", Bytes: 1, CountedAt: at}))
	require.NoError(t, st.Put(ctx, Size{Dir: "/trees/b", Bytes: 2, CountedAt: at}))

	require.NoError(t, st.Delete(ctx, nil))
	require.NoError(t, st.Delete(ctx, []string{"/trees/a", "/trees/missing"}))

	sizes, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, sizes, 1)
	assert.Contains(t, sizes, "/trees/b")
}
