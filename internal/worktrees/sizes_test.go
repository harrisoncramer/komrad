package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	worktreestore "weave/internal/store/worktree"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCounter(f *fixture, refresh time.Duration) *Counter {
	return NewCounter(NewCounterParams{Lister: f.lister, Sizes: f.sizes, Refresh: refresh})
}

// TestCountSumsEveryFileUnderADirectory covers the walk counting regular files through
// subdirectories, following no symlink, and keeping what each top-level entry holds.
func TestCountSumsEveryFileUnderADirectory(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.shared, "aaaa")
	write(t, dir, "one", strings.Repeat("x", 100))
	write(t, dir, "deep/two", strings.Repeat("x", 50))
	write(t, dir, "deep/three", strings.Repeat("x", 75))
	require.NoError(t, os.Symlink(f.repo, filepath.Join(dir, "linked")))

	require.NoError(t, newCounter(f, time.Hour).Count(t.Context()))

	require.Contains(t, f.sizes.sizes, dir)
	assert.Equal(t, int64(225), f.sizes.sizes[dir].Bytes)
	assert.False(t, f.sizes.sizes[dir].CountedAt.IsZero())
	assert.Equal(t, []worktreestore.Entry{
		{Path: "deep", Bytes: 125, Dir: true},
		{Path: "one", Bytes: 100},
		{Path: "linked", Bytes: 0},
	}, f.sizes.sizes[dir].Entries)
}

// TestCountRefreshesStaleCountsOnlyOnceSomebodyHasListed covers the refresh waiting for a
// listing: a stale count stays until the lister has been asked since the last pass, while a
// directory with no count is taken either way.
func TestCountRefreshesStaleCountsOnlyOnceSomebodyHasListed(t *testing.T) {
	f := newFixture(t)
	fresh := filepath.Join(f.shared, "fresh")
	stale := filepath.Join(f.shared, "stale")
	unknown := filepath.Join(f.shared, "unknown")
	write(t, fresh, "one", "x")
	write(t, stale, "one", "x")
	write(t, unknown, "one", "xx")
	now := time.Now()
	f.sizes.sizes[fresh] = worktreestore.Size{Dir: fresh, Bytes: 999, CountedAt: now}
	f.sizes.sizes[stale] = worktreestore.Size{Dir: stale, Bytes: 999, CountedAt: now.Add(-time.Hour)}
	counter := newCounter(f, 10*time.Minute)

	_, err := f.lister.List(t.Context())
	require.NoError(t, err)
	require.NoError(t, counter.Count(t.Context()))

	assert.Equal(t, int64(999), f.sizes.sizes[fresh].Bytes)
	assert.Equal(t, int64(1), f.sizes.sizes[stale].Bytes)
	assert.Equal(t, int64(2), f.sizes.sizes[unknown].Bytes)

	f.sizes.sizes[stale] = worktreestore.Size{Dir: stale, Bytes: 999, CountedAt: now.Add(-time.Hour)}
	delete(f.sizes.sizes, unknown)
	require.NoError(t, counter.Count(t.Context()))

	assert.Equal(t, int64(999), f.sizes.sizes[stale].Bytes)
	assert.Equal(t, int64(2), f.sizes.sizes[unknown].Bytes)
}

// TestCountDropsTheCountOfAGoneDirectory covers a row whose directory is no longer under the
// worktree directories being deleted by the pass, and one under a directory the pass could
// not read being kept.
func TestCountDropsTheCountOfAGoneDirectory(t *testing.T) {
	f := newFixture(t)
	gone := filepath.Join(f.shared, "gone")
	f.sizes.sizes[gone] = worktreestore.Size{Dir: gone, Bytes: 5, CountedAt: time.Now()}
	unreadable := filepath.Join(filepath.Dir(f.shared), "not-a-dir")
	write(t, filepath.Dir(f.shared), "not-a-dir", "x\n")
	kept := filepath.Join(unreadable, "kept")
	f.sizes.sizes[kept] = worktreestore.Size{Dir: kept, Bytes: 5, CountedAt: time.Now()}
	f.flows.dirs = []string{unreadable}

	require.NoError(t, newCounter(f, time.Hour).Count(t.Context()))

	assert.NotContains(t, f.sizes.sizes, gone)
	assert.Contains(t, f.sizes.sizes, kept)
}

// TestMeasureGivesUpWhenTheContextEnds covers a walk that hangs being abandoned rather than
// waited out.
func TestMeasureGivesUpWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := measure(ctx, t.TempDir())

	require.ErrorIs(t, err, context.Canceled)
}

// TestMeasureReportsADirectoryThatIsNotThere covers the one walk error that is not skipped,
// the root itself being gone, which the counter reads as nothing to record.
func TestMeasureReportsADirectoryThatIsNotThere(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "kept", "xx")

	size, _, err := measure(t.Context(), dir)
	require.NoError(t, err)
	assert.Equal(t, int64(2), size)

	_, _, err = measure(t.Context(), filepath.Join(dir, "never"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
