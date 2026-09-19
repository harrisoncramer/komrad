package worktrees

import (
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
// subdirectories and following no symlink.
func TestCountSumsEveryFileUnderADirectory(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.shared, "aaaa")
	write(t, dir, "one", strings.Repeat("x", 100))
	write(t, dir, "deep/two", strings.Repeat("x", 50))
	require.NoError(t, os.Symlink(f.repo, filepath.Join(dir, "linked")))

	require.NoError(t, newCounter(f, time.Hour).Count(t.Context()))

	require.Contains(t, f.sizes.sizes, dir)
	assert.Equal(t, int64(150), f.sizes.sizes[dir].Bytes)
	assert.False(t, f.sizes.sizes[dir].CountedAt.IsZero())
}

// TestCountLeavesAFreshCountAlone covers a count younger than the refresh not being taken
// again, and one older than it being replaced.
func TestCountLeavesAFreshCountAlone(t *testing.T) {
	f := newFixture(t)
	fresh := filepath.Join(f.shared, "fresh")
	stale := filepath.Join(f.shared, "stale")
	write(t, fresh, "one", "x")
	write(t, stale, "one", "x")
	now := time.Now()
	f.sizes.sizes[fresh] = worktreestore.Size{Dir: fresh, Bytes: 999, CountedAt: now}
	f.sizes.sizes[stale] = worktreestore.Size{Dir: stale, Bytes: 999, CountedAt: now.Add(-time.Hour)}

	require.NoError(t, newCounter(f, 10*time.Minute).Count(t.Context()))

	assert.Equal(t, int64(999), f.sizes.sizes[fresh].Bytes)
	assert.Equal(t, int64(1), f.sizes.sizes[stale].Bytes)
}

// TestCountDropsTheCountOfAGoneDirectory covers a row whose directory is no longer under the
// worktree directories being deleted by the pass.
func TestCountDropsTheCountOfAGoneDirectory(t *testing.T) {
	f := newFixture(t)
	gone := filepath.Join(f.shared, "gone")
	f.sizes.sizes[gone] = worktreestore.Size{Dir: gone, Bytes: 5, CountedAt: time.Now()}

	require.NoError(t, newCounter(f, time.Hour).Count(t.Context()))

	assert.NotContains(t, f.sizes.sizes, gone)
}

// TestMeasureReportsADirectoryThatIsNotThere covers the one walk error that is not skipped,
// the root itself being gone, which the counter reads as nothing to record.
func TestMeasureReportsADirectoryThatIsNotThere(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "kept", "xx")

	size, err := measure(t.Context(), dir)
	require.NoError(t, err)
	assert.Equal(t, int64(2), size)

	_, err = measure(t.Context(), filepath.Join(dir, "never"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
