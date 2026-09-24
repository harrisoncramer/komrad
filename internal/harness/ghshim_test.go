package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteGHShimNeedsSomewhereToWriteAndSomethingToRun covers a launch with no cache dir or no
// komrade binary, which gets no stand-in rather than a broken one.
func TestWriteGHShimNeedsSomewhereToWriteAndSomethingToRun(t *testing.T) {
	dir, err := WriteGHShim("", "/usr/local/bin/komrade")
	require.NoError(t, err)
	assert.Empty(t, dir)

	dir, err = WriteGHShim(t.TempDir(), "")
	require.NoError(t, err)
	assert.Empty(t, dir)
}

// TestWriteGHShimReplacesTheFileItIsRunFrom covers a second launch rewriting the stand-in
// while another agent may be running it.
func TestWriteGHShimReplacesTheFileItIsRunFrom(t *testing.T) {
	cache := t.TempDir()

	dir, err := WriteGHShim(cache, "/usr/local/bin/komrade")
	require.NoError(t, err)
	first, err := os.Stat(filepath.Join(dir, "gh"))
	require.NoError(t, err)

	_, err = WriteGHShim(cache, "/usr/local/bin/komrade")
	require.NoError(t, err)

	second, err := os.Stat(filepath.Join(dir, "gh"))
	require.NoError(t, err)
	assert.False(t, os.SameFile(first, second), "the stand-in is renamed over rather than truncated in place")
	assert.Equal(t, os.FileMode(0o755), second.Mode().Perm())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "the file it was written through is cleaned up")
}
