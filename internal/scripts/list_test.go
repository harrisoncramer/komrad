package scripts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListSeesAChmod covers the sharp end of listing a hook: the listing tells a person the file
// cannot run until they chmod it, so the chmod has to be what the next listing reports.
func TestListSeesAChmod(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "open-pr")
	write(t, path, 0o644)

	first, err := List(dir)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.False(t, first[0].Executable)

	require.NoError(t, os.Chmod(path, 0o755))

	second, err := List(dir)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.True(t, second[0].Executable)
}

// TestListSeesAnEditedHeader covers editing a file already in the directory, which leaves the
// directory itself untouched.
func TestListSeesAnEditedHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "open-pr")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n#WEAVE description=\"before\"\n"), 0o755))

	first, err := List(dir)
	require.NoError(t, err)
	require.Len(t, first, 1)
	require.Equal(t, "before", first[0].Header.Description)

	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n#WEAVE description=\"after\"\n"), 0o755))

	second, err := List(dir)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, "after", second[0].Header.Description)
}

// write puts an empty script at the path.
func write(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), mode))
}
