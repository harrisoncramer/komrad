package cookbooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstallBuiltins covers the cookbooks weave ships reaching disk as real directories,
// which Discover needs since it reads a real filesystem rather than the embedded one.
func TestInstallBuiltins(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cookbooks")

	require.NoError(t, InstallBuiltins(dir))

	found, err := Discover(dir)
	require.NoError(t, err)
	require.NotEmpty(t, found, "this build ships at least one cookbook")

	names := make([]string, 0, len(found))
	for _, cookbook := range found {
		names = append(names, cookbook.Name)
	}

	assert.Contains(t, names, "worktree-agent")
	assert.Contains(t, names, "triage-schedule")
	assert.Contains(t, names, "pr-reviewer")
	assert.NotContains(t, names, markerName, "the file claiming the directory is not offered as a cookbook")
}

// TestInstallBuiltinsReplacesWhatWasThere covers a build that no longer ships a cookbook. The
// directory is the service's own, so what an older build left in it is cleared rather than
// staying on offer.
func TestInstallBuiltinsReplacesWhatWasThere(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cookbooks")
	require.NoError(t, InstallBuiltins(dir))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "gone"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "gone", "cookbook.toml"), nil, 0o644))

	require.NoError(t, InstallBuiltins(dir))

	_, err := os.Stat(filepath.Join(dir, "gone"))
	assert.True(t, os.IsNotExist(err), "a cookbook this build does not ship is not left behind")
}

// TestInstallBuiltinsRefusesRelativePath covers the directory the install empties being named
// relative to wherever the caller happened to be started, which is a delete of whatever that
// name means there rather than of weave's own directory.
func TestInstallBuiltinsRefusesRelativePath(t *testing.T) {
	work := t.TempDir()
	t.Chdir(work)

	theirs := filepath.Join(work, "cookbooks")
	require.NoError(t, os.MkdirAll(theirs, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(theirs, "deploy.toml"), nil, 0o644))

	err := InstallBuiltins(filepath.Join(".", "cookbooks"))
	require.ErrorIs(t, err, ErrDirNotAbsolute)

	assert.FileExists(t, filepath.Join(theirs, "deploy.toml"), "a directory weave did not name is not emptied")
}

// TestInstallBuiltinsRefusesDirectoryItDoesNotOwn covers an absolute path that is somebody
// else's directory, which is what a hand-set cache directory pointed at a project root leaves
// the install aimed at.
func TestInstallBuiltinsRefusesDirectoryItDoesNotOwn(t *testing.T) {
	theirs := filepath.Join(t.TempDir(), "cookbooks")
	require.NoError(t, os.MkdirAll(theirs, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(theirs, "deploy.toml"), nil, 0o644))

	err := InstallBuiltins(theirs)
	require.ErrorIs(t, err, ErrDirNotOurs)

	assert.FileExists(t, filepath.Join(theirs, "deploy.toml"), "a directory weave did not write is not emptied")
}
