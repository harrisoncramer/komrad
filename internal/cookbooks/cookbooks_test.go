package cookbooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeCookbook writes a minimal cookbook.toml under dir/name, and an action file alongside
// it when actionFile is not empty.
func writeCookbook(t *testing.T, dir, name, description, actionFile string) {
	t.Helper()

	cookbookDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(cookbookDir, 0o755))

	manifest := "description = \"" + description + "\"\n\n[workflow]\nname = \"" + name + "\"\n"
	if actionFile != "" {
		manifest += "\n[[actions]]\nname = \"" + actionFile + "\"\nfile = \"" + actionFile + "\"\n"
		require.NoError(t, os.WriteFile(filepath.Join(cookbookDir, actionFile), []byte("#!/bin/sh\n"), 0o644))
	}

	require.NoError(t, os.WriteFile(filepath.Join(cookbookDir, manifestName), []byte(manifest), 0o644))
}

// TestDiscoverMissingDir covers a cookbooks directory nobody has written, which has to read
// as no cookbooks rather than an error, since not configuring one is the ordinary case.
func TestDiscoverMissingDir(t *testing.T) {
	found, err := Discover(filepath.Join(t.TempDir(), "does-not-exist"))

	require.NoError(t, err)
	assert.Empty(t, found)
}

// TestDiscoverSkipsEntriesWithoutAManifest covers a subdirectory that is not a cookbook, such
// as a stray file or a directory somebody dropped in without a cookbook.toml.
func TestDiscoverSkipsEntriesWithoutAManifest(t *testing.T) {
	dir := t.TempDir()
	writeCookbook(t, dir, "real", "a real one", "")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "empty"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stray-file"), nil, 0o644))

	found, err := Discover(dir)
	require.NoError(t, err)

	require.Len(t, found, 1)
	assert.Equal(t, "real", found[0].Name)
}

// TestListUserOverridesBuiltin covers the override order List and Find both promise: a
// cookbook in an earlier directory wins over one of the same name in a later one, which is
// what lets a user's own directory replace a built-in.
func TestListUserOverridesBuiltin(t *testing.T) {
	userDir, builtinDir := t.TempDir(), t.TempDir()
	writeCookbook(t, builtinDir, "worktree-agent", "the built-in", "")
	writeCookbook(t, userDir, "worktree-agent", "the user's own", "")
	writeCookbook(t, builtinDir, "pr-reviewer", "only in builtin", "")

	found, err := List(userDir, builtinDir)
	require.NoError(t, err)

	require.Len(t, found, 2)
	assert.Equal(t, "the user's own", found[1].Manifest.Description)
	assert.Equal(t, "only in builtin", found[0].Manifest.Description)
}

// TestFindNotFound covers asking for a cookbook nowhere in the given directories, which has
// to name what was asked for rather than reading as an empty list.
func TestFindNotFound(t *testing.T) {
	_, err := Find("missing", t.TempDir())

	require.ErrorIs(t, err, ErrCookbookNotFound)
	assert.Contains(t, err.Error(), "missing")
}

// TestFindReadsTheManifest covers a lookup by name resolving the full manifest, not just the
// cookbook's identity.
func TestFindReadsTheManifest(t *testing.T) {
	dir := t.TempDir()
	writeCookbook(t, dir, "triage-schedule", "on-call triage", "")

	found, err := Find("triage-schedule", dir)
	require.NoError(t, err)

	assert.Equal(t, "on-call triage", found.Manifest.Description)
	assert.Equal(t, "triage-schedule", found.Manifest.Workflow.Name)
}

// TestInstallActionsWritesExecutableFiles covers the actions a cookbook declares reaching the
// actions directory under their own name and made runnable, since running one is an exec of a
// path and a file without the executable bit would otherwise sit there unusable.
func TestInstallActionsWritesExecutableFiles(t *testing.T) {
	dir := t.TempDir()
	writeCookbook(t, dir, "worktree-agent", "one worktree per agent", "worktree-agent-setup")

	found, err := Find("worktree-agent", dir)
	require.NoError(t, err)

	actionsDir := filepath.Join(t.TempDir(), "actions")
	installed, err := found.InstallActions(actionsDir)
	require.NoError(t, err)

	assert.Equal(t, []string{"worktree-agent-setup"}, installed)

	info, err := os.Stat(filepath.Join(actionsDir, "worktree-agent-setup"))
	require.NoError(t, err)
	assert.NotZero(t, info.Mode().Perm()&0o111, "the installed action is executable")
}

// TestInstallActionsMissingFile covers a manifest naming a sibling file that is not there,
// which has to name the cookbook and the missing file rather than a bare "no such file".
func TestInstallActionsMissingFile(t *testing.T) {
	dir := t.TempDir()
	cookbookDir := filepath.Join(dir, "broken")
	require.NoError(t, os.MkdirAll(cookbookDir, 0o755))
	manifest := "[[actions]]\nname = \"gone\"\nfile = \"gone\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(cookbookDir, manifestName), []byte(manifest), 0o644))

	found, err := Find("broken", dir)
	require.NoError(t, err)

	_, err = found.InstallActions(filepath.Join(t.TempDir(), "actions"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "broken")
	assert.Contains(t, err.Error(), "gone")
}
