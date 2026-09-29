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
	dir := t.TempDir()
	write(t, dir, "one", strings.Repeat("x", 100))
	write(t, dir, "deep/two", strings.Repeat("x", 50))
	write(t, dir, "deep/down/three", strings.Repeat("x", 75))
	write(t, dir, "empty", "")
	require.NoError(t, os.Symlink(t.TempDir(), filepath.Join(dir, "linked")))
	one := usage(t, filepath.Join(dir, "one"))
	deep := usage(t, filepath.Join(dir, "deep/two")) + usage(t, filepath.Join(dir, "deep/down/three"))
	require.Positive(t, one)

	measured, err := measure(t.Context(), dir)
	require.NoError(t, err)

	assert.Equal(t, one+deep, measured.bytes)
	assert.Equal(t, measured.bytes, measured.share)
	assert.Equal(t, []Entry{
		{
			Path:  "deep",
			Bytes: deep,
			Dir:   true,
		},
		{Path: "one", Bytes: one},
		{Path: "empty", Bytes: 0},
		{Path: "linked", Bytes: 0},
	}, measured.entries)
}

// TestMeasureCountsBlocksRatherThanApparentSize covers a sparse file, whose apparent size is a
// megabyte and whose blocks are none.
func TestMeasureCountsBlocksRatherThanApparentSize(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "sparse", "")
	require.NoError(t, os.Truncate(filepath.Join(dir, "sparse"), 1<<20))

	measured, err := measure(t.Context(), dir)
	require.NoError(t, err)

	assert.Less(t, measured.bytes, int64(1<<20))
	assert.Equal(t, []Entry{{Path: "sparse", Bytes: measured.bytes}}, measured.entries)
}

// TestMeasureCountsAHardLinkedFileOnce covers two links to one file adding up to one file,
// charged to the first top-level entry in name order.
func TestMeasureCountsAHardLinkedFileOnce(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "b/shared", strings.Repeat("x", 100))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "a"), 0o755))
	require.NoError(t, os.Link(filepath.Join(dir, "b/shared"), filepath.Join(dir, "a/shared")))
	shared := usage(t, filepath.Join(dir, "b/shared"))

	measured, err := measure(t.Context(), dir)
	require.NoError(t, err)

	assert.Equal(t, shared, measured.bytes)
	assert.Equal(t, shared, measured.share)
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
	}, measured.entries)
}

// TestMeasureSplitsAFileSharedWithAnotherDirectory covers each directory taking its links' part of a shared file.
func TestMeasureSplitsAFileSharedWithAnotherDirectory(t *testing.T) {
	root := t.TempDir()
	write(t, root, "left/own", strings.Repeat("x", 100))
	write(t, root, "left/shared", strings.Repeat("x", 5000))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "right/nested"), 0o755))
	require.NoError(t, os.Link(filepath.Join(root, "left/shared"), filepath.Join(root, "right/shared")))
	require.NoError(t, os.Link(filepath.Join(root, "left/shared"), filepath.Join(root, "right/nested/shared")))
	own := usage(t, filepath.Join(root, "left/own"))
	shared := usage(t, filepath.Join(root, "left/shared"))

	left, err := measure(t.Context(), filepath.Join(root, "left"))
	require.NoError(t, err)
	right, err := measure(t.Context(), filepath.Join(root, "right"))
	require.NoError(t, err)

	assert.Equal(t, own+shared, left.bytes)
	assert.Equal(t, shared, right.bytes)
	assert.Equal(t, own+shared/3, left.share)
	assert.Equal(t, shared*2/3, right.share)
	assert.InDelta(t, own+shared, left.share+right.share, 1)
}

// TestMeasureGivesUpWhenTheContextEnds covers the walk stopping once the context has ended.
func TestMeasureGivesUpWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := measure(ctx, t.TempDir())

	require.ErrorIs(t, err, context.Canceled)
}

// TestMeasureReportsADirectoryThatIsNotThere covers the one walk error that is not skipped,
// the root itself being gone.
func TestMeasureReportsADirectoryThatIsNotThere(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "kept", "xx")

	measured, err := measure(t.Context(), dir)
	require.NoError(t, err)
	assert.Equal(t, usage(t, filepath.Join(dir, "kept")), measured.bytes)

	_, err = measure(t.Context(), filepath.Join(dir, "never"))
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
