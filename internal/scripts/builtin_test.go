package scripts

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstallBuiltins covers the scripts weave ships reaching disk, each underscored since
// they sit behind the panel's own keys rather than in the list a person picks from.
func TestInstallBuiltins(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "scripts")

	require.NoError(t, InstallBuiltins(dir))

	found, err := discover(dir)
	require.NoError(t, err)
	require.NotEmpty(t, found, "this build ships at least the worktree provisioner")

	names := make([]string, 0, len(found))
	for _, script := range found {
		names = append(names, script.Name)
		assert.True(t, script.Executable, "%s reached disk without the executable bit", script.Name)
	}

	assert.Contains(t, names, "_worktree_setup")
	assert.NotContains(t, names, markerName, "the file claiming the directory is not something to run")
}

// TestInstallBuiltinsReplacesWhatWasThere covers a build that no longer ships a script, where
// what an older build left is cleared rather than staying on offer.
func TestInstallBuiltinsReplacesWhatWasThere(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "scripts")
	require.NoError(t, InstallBuiltins(dir))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "_gone"), []byte("#!/bin/sh\n"), 0o755))

	require.NoError(t, InstallBuiltins(dir))

	_, err := os.Stat(filepath.Join(dir, "_gone"))
	assert.True(t, os.IsNotExist(err), "a script this build does not ship is not left behind")
}

// TestInstallBuiltinsRefusesRelativePath covers the directory the install empties being
// named relative to wherever the service happened to be started, which is a delete of
// whatever that name means there rather than of weave's own directory.
func TestInstallBuiltinsRefusesRelativePath(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)

	theirs := filepath.Join(work, "scripts")
	require.NoError(t, os.MkdirAll(theirs, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(theirs, "deploy.sh"), []byte("#!/bin/sh\n"), 0o755))

	err := InstallBuiltins(filepath.Join(".", "scripts"))
	require.ErrorIs(t, err, ErrDirNotAbsolute)

	assert.FileExists(t, filepath.Join(theirs, "deploy.sh"), "a directory weave did not name is not emptied")
}

// TestInstallBuiltinsRefusesDirectoryItDoesNotOwn covers an absolute path that is somebody
// else's directory, which is what a hand-set cache directory pointed at a project root
// leaves the install aimed at.
func TestInstallBuiltinsRefusesDirectoryItDoesNotOwn(t *testing.T) {
	theirs := filepath.Join(t.TempDir(), "scripts")
	require.NoError(t, os.MkdirAll(theirs, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(theirs, "deploy.sh"), []byte("#!/bin/sh\n"), 0o755))

	err := InstallBuiltins(theirs)
	require.ErrorIs(t, err, ErrDirNotOurs)

	assert.FileExists(t, filepath.Join(theirs, "deploy.sh"), "a directory weave did not write is not emptied")
}
