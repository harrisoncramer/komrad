package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMeasureSumsEveryFileUnderADirectory covers the walk counting regular files through
// subdirectories, following no symlink, and keeping what each top-level entry holds.
func TestMeasureSumsEveryFileUnderADirectory(t *testing.T) {
	f := newFixture(t)
	dir := filepath.Join(f.shared, "aaaa")
	write(t, dir, "one", strings.Repeat("x", 100))
	write(t, dir, "deep/two", strings.Repeat("x", 50))
	write(t, dir, "deep/down/three", strings.Repeat("x", 75))
	write(t, dir, "empty", "")
	require.NoError(t, os.Symlink(f.repo, filepath.Join(dir, "linked")))
	one := usage(t, filepath.Join(dir, "one"))
	deep := usage(t, filepath.Join(dir, "deep/two")) + usage(t, filepath.Join(dir, "deep/down/three"))
	require.Positive(t, one)

	total, entries, err := measure(t.Context(), dir)
	require.NoError(t, err)

	assert.Equal(t, one+deep, total)
	assert.Equal(t, []Entry{
		{
			Path:  "deep",
			Bytes: deep,
			Dir:   true,
		},
		{Path: "one", Bytes: one},
		{Path: "empty", Bytes: 0},
		{Path: "linked", Bytes: 0},
	}, entries)
}

// TestMeasureCountsBlocksRatherThanApparentSize covers a sparse file, whose apparent size is a
// megabyte and whose blocks are none.
func TestMeasureCountsBlocksRatherThanApparentSize(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sparse", "")
	require.NoError(t, os.Truncate(filepath.Join(dir, "sparse"), 1<<20))

	total, entries, err := measure(t.Context(), dir)
	require.NoError(t, err)

	assert.Less(t, total, int64(1<<20))
	assert.Equal(t, []Entry{{Path: "sparse", Bytes: total}}, entries)
}

// TestMeasureCountsAHardLinkedFileOnce covers two links to one file adding up to one file,
// charged to the first top-level entry in name order.
func TestMeasureCountsAHardLinkedFileOnce(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "b/shared", strings.Repeat("x", 100))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a"), 0o755))
	require.NoError(t, os.Link(filepath.Join(dir, "b/shared"), filepath.Join(dir, "a/shared")))
	shared := usage(t, filepath.Join(dir, "b/shared"))

	total, entries, err := measure(t.Context(), dir)
	require.NoError(t, err)

	assert.Equal(t, shared, total)
	assert.Equal(t, []Entry{
		{
			Path:  "a",
			Bytes: shared,
			Dir:   true,
		},
		{
			Path:  "b",
			Bytes: 0,
			Dir:   true,
		},
	}, entries)
}

// TestMeasureGivesUpWhenTheContextEnds covers the walk stopping once the context has ended.
func TestMeasureGivesUpWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := measure(ctx, t.TempDir())

	require.ErrorIs(t, err, context.Canceled)
}

// TestMeasureReportsADirectoryThatIsNotThere covers the one walk error that is not skipped,
// the root itself being gone.
func TestMeasureReportsADirectoryThatIsNotThere(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "kept", "xx")

	size, _, err := measure(t.Context(), dir)
	require.NoError(t, err)
	assert.Equal(t, usage(t, filepath.Join(dir, "kept")), size)

	_, _, err = measure(t.Context(), filepath.Join(dir, "never"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

// usage is the disk one file takes, read off its blocks the way the walk reads them.
func usage(t *testing.T, path string) int64 {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)
	stat, ok := info.Sys().(*syscall.Stat_t)
	require.True(t, ok)

	return stat.Blocks * blockSize
}
