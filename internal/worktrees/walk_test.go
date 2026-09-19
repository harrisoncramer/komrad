package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	write(t, dir, "deep/three", strings.Repeat("x", 75))
	require.NoError(t, os.Symlink(f.repo, filepath.Join(dir, "linked")))

	total, entries, err := measure(t.Context(), dir)
	require.NoError(t, err)

	assert.Equal(t, int64(225), total)
	assert.Equal(t, []Entry{
		{Path: "deep", Bytes: 125, Dir: true},
		{Path: "one", Bytes: 100},
		{Path: "linked", Bytes: 0},
	}, entries)
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
// the root itself being gone.
func TestMeasureReportsADirectoryThatIsNotThere(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "kept", "xx")

	size, _, err := measure(t.Context(), dir)
	require.NoError(t, err)
	assert.Equal(t, int64(2), size)

	_, _, err = measure(t.Context(), filepath.Join(dir, "never"))
	require.ErrorIs(t, err, os.ErrNotExist)
}
