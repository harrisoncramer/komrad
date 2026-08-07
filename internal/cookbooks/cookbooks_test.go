package cookbooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeCookbook writes a minimal cookbook.toml under dir/name, and a sibling action file named
// actionFile alongside it when actionFile is not empty.
func writeCookbook(t *testing.T, dir, name, description, actionFile string) {
	t.Helper()

	cookbookDir := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(cookbookDir, 0o755))

	manifest := "description = \"" + description + "\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(cookbookDir, manifestName), []byte(manifest), 0o644))

	if actionFile != "" {
		require.NoError(t, os.WriteFile(filepath.Join(cookbookDir, actionFile), []byte("#!/bin/sh\n"), 0o644))
	}
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

// TestDiscoverKeepsTheGoodCookbookNextToABadOne covers a malformed cookbook.toml no longer
// hiding every other cookbook in the same directory: Discover still returns the broken one,
// with ManifestErr set, rather than aborting the whole scan.
func TestDiscoverKeepsTheGoodCookbookNextToABadOne(t *testing.T) {
	dir := t.TempDir()
	writeCookbook(t, dir, "good", "a perfectly fine cookbook", "")

	broken := filepath.Join(dir, "broken")
	require.NoError(t, os.MkdirAll(broken, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(broken, manifestName), []byte("not valid toml [[[\n"), 0o644))

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 2)

	byName := make(map[string]Cookbook, len(found))
	for _, cookbook := range found {
		byName[cookbook.Name] = cookbook
	}

	assert.NoError(t, byName["good"].ManifestErr)
	assert.Error(t, byName["broken"].ManifestErr)
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
}

// TestInstalledActionNamePrefixesWithTheCookbookName covers two cookbooks whose action files
// share a generic name, such as "setup", not colliding once installed.
func TestInstalledActionNamePrefixesWithTheCookbookName(t *testing.T) {
	cookbook := Cookbook{Name: "worktree-agent"}

	assert.Equal(t, "worktree-agent-setup", cookbook.InstalledActionName("setup"))
}

// TestInstallActionsWritesExecutableFiles covers the actions a cookbook declares reaching the
// actions directory under their prefixed name and made runnable, since running one is an exec
// of a path and a file without the executable bit would otherwise sit there unusable.
func TestInstallActionsWritesExecutableFiles(t *testing.T) {
	dir := t.TempDir()
	writeCookbook(t, dir, "worktree-agent", "one worktree per agent", "setup")

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

// TestInstallActionsSkipsTheManifestItself covers cookbook.toml never being offered as an
// action alongside whatever scripts actually sit next to it.
func TestInstallActionsSkipsTheManifestItself(t *testing.T) {
	dir := t.TempDir()
	writeCookbook(t, dir, "worktree-agent", "one worktree per agent", "setup")

	found, err := Find("worktree-agent", dir)
	require.NoError(t, err)

	installed, err := found.InstallActions(filepath.Join(t.TempDir(), "actions"))
	require.NoError(t, err)

	assert.Equal(t, []string{"worktree-agent-setup"}, installed)
}

// TestValidate covers what Validate does and does not object to.
func TestValidate(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		files    map[string]string
		wantErr  string
	}{
		{
			name:     "a manifest that will not parse",
			manifest: "not valid toml [[[\n",
			wantErr:  "failed to parse",
		},
		{
			name:     "setup_action names a file that is not there",
			manifest: "[workflow]\nsetup_action = \"nope\"\n",
			wantErr:  `"nope"`,
		},
		{
			name:     "cleanup_action names a file that is not there",
			manifest: "[workflow]\ncleanup_action = \"nope\"\n",
			wantErr:  `"nope"`,
		},
		{
			name:     "a sibling file with no shebang",
			manifest: "",
			files:    map[string]string{"notes.txt": "just some prose\n"},
			wantErr:  "notes.txt",
		},
		{
			name:     "a plain cookbook with no actions",
			manifest: "description = \"fine\"\n",
		},
		{
			name:     "setup_action naming a real sibling script",
			manifest: "[workflow]\nsetup_action = \"setup\"\n",
			files:    map[string]string{"setup": "#!/bin/sh\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cookbookDir := filepath.Join(dir, "under-test")
			require.NoError(t, os.MkdirAll(cookbookDir, 0o755))
			require.NoError(t, os.WriteFile(filepath.Join(cookbookDir, manifestName), []byte(tt.manifest), 0o644))

			for file, content := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(cookbookDir, file), []byte(content), 0o644))
			}

			found, err := Find("under-test", dir)
			require.NoError(t, err, "Find succeeds even for a manifest that will not parse")

			err = found.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestInstallActionsWithNoSiblingFiles covers a cookbook that is only a workflow and,
// optionally, a schedule, such as triage-schedule: installing it writes nothing.
func TestInstallActionsWithNoSiblingFiles(t *testing.T) {
	dir := t.TempDir()
	writeCookbook(t, dir, "triage-schedule", "on-call triage", "")

	found, err := Find("triage-schedule", dir)
	require.NoError(t, err)

	installed, err := found.InstallActions(filepath.Join(t.TempDir(), "actions"))
	require.NoError(t, err)

	assert.Empty(t, installed)
}
