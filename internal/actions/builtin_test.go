package actions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstallBuiltins covers the actions weave ships reaching disk, which they have to do
// because running one is an exec of a path. Every one is underscored, since they sit behind
// the panel's own keys rather than in the list a person picks from.
func TestInstallBuiltins(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "actions")

	require.NoError(t, InstallBuiltins(dir))

	found, err := NewRunner().Discover(dir)
	require.NoError(t, err)
	require.NotEmpty(t, found, "this build ships at least the editor and the shell")

	names := make([]string, 0, len(found))
	for _, action := range found {
		names = append(names, action.Name)
		assert.True(t, action.Executable, "%s reached disk without the executable bit", action.Name)
	}

	assert.Contains(t, names, "_editor")
	assert.Contains(t, names, "_terminal")
}

// TestInstallBuiltinsReplacesWhatWasThere covers a build that no longer ships a script. The
// directory is the service's own, so what an older build left in it is cleared rather than
// staying on offer.
func TestInstallBuiltinsReplacesWhatWasThere(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "actions")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_gone"), []byte("#!/bin/sh\n"), 0o755))

	require.NoError(t, InstallBuiltins(dir))

	_, err := os.Stat(filepath.Join(dir, "_gone"))
	assert.True(t, os.IsNotExist(err), "a script this build does not ship is not left behind")
}
