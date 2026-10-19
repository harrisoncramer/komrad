package harness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteToolShimsNeedsSomewhereToWriteAndSomethingToRun covers a launch with no cache dir or no
// komrad binary, which gets no stand-ins rather than broken ones.
func TestWriteToolShimsNeedsSomewhereToWriteAndSomethingToRun(t *testing.T) {
	dir, err := WriteToolShims(WriteToolShimsParams{KomradBinary: "/usr/local/bin/komrad"})
	require.NoError(t, err)
	assert.Empty(t, dir)

	dir, err = WriteToolShims(WriteToolShimsParams{CacheDir: t.TempDir()})
	require.NoError(t, err)
	assert.Empty(t, dir)
}

// TestWriteToolShimsReplacesTheFileItIsRunFrom covers a second launch rewriting the stand-ins
// while another agent may be running one.
func TestWriteToolShimsReplacesTheFileItIsRunFrom(t *testing.T) {
	cache := t.TempDir()

	dir, err := WriteToolShims(WriteToolShimsParams{CacheDir: cache, KomradBinary: "/usr/local/bin/komrad"})
	require.NoError(t, err)
	first, err := os.Stat(filepath.Join(dir, "gh"))
	require.NoError(t, err)

	_, err = WriteToolShims(WriteToolShimsParams{CacheDir: cache, KomradBinary: "/usr/local/bin/komrad"})
	require.NoError(t, err)

	second, err := os.Stat(filepath.Join(dir, "gh"))
	require.NoError(t, err)
	assert.False(t, os.SameFile(first, second), "the stand-in is renamed over rather than truncated in place")
	assert.Equal(t, os.FileMode(0o700), second.Mode().Perm())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "the files they were written through are cleaned up")
}
