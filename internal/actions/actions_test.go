package actions

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDiscover covers what the directory offers as an action and what it passes over. A file
// that cannot be run is still listed, because a script that was written and then never appears
// reads as the panel being broken rather than as a file wanting chmod.
func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "open-pr"), 0o755)
	write(t, filepath.Join(dir, "not-executable"), 0o644)
	write(t, filepath.Join(dir, ".open-pr.swp"), 0o755)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "helpers"), 0o755))
	write(t, filepath.Join(dir, "helpers", "nested"), 0o755)
	require.NoError(t, os.Symlink(filepath.Join(dir, "open-pr"), filepath.Join(dir, "linked")))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "fifo"), 0o755))

	found, err := Discover(dir)
	require.NoError(t, err)

	listed := map[string]Action{}
	names := make([]string, 0, len(found))
	for _, action := range found {
		listed[action.Name] = action
		names = append(names, action.Name)
	}

	tests := []struct {
		name           string
		entry          string
		wantListed     bool
		wantExecutable bool
	}{
		{
			name:       "an editor's leftover is not an action",
			entry:      ".open-pr.swp",
			wantListed: false,
		},
		{
			name:       "a subdirectory is skipped rather than walked",
			entry:      "helpers",
			wantListed: false,
		},
		{
			// Reading one waits for a writer that is never coming, so listing it would
			// hang whoever reads the file rather than fail.
			name:       "a fifo is not an action, however its mode reads",
			entry:      "fifo",
			wantListed: false,
		},
		{
			name:       "a nested file is not reached",
			entry:      "nested",
			wantListed: false,
		},
		{
			name:           "a file without the bit is listed and will not run",
			entry:          "not-executable",
			wantListed:     true,
			wantExecutable: false,
		},
		{
			name:           "a symlink carries the mode of the file it points at",
			entry:          "linked",
			wantListed:     true,
			wantExecutable: true,
		},
		{
			name:           "an executable file is an action",
			entry:          "open-pr",
			wantListed:     true,
			wantExecutable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, ok := listed[tt.entry]
			require.Equal(t, tt.wantListed, ok)
			if !tt.wantListed {
				return
			}

			assert.Equal(t, tt.wantExecutable, action.Executable)
			assert.Equal(t, filepath.Join(dir, tt.entry), action.Path)
		})
	}

	assert.Equal(t, []string{"linked", "not-executable", "open-pr"}, names, "listed by name")
}

// TestDiscoverMissingDirectory covers the ordinary case of never having written one.
// Nothing is wrong, so nothing is reported: the panel says where it looked on screen.
func TestDiscoverMissingDirectory(t *testing.T) {
	found, err := Discover(filepath.Join(t.TempDir(), "never-created"))

	require.NoError(t, err)
	assert.Empty(t, found)
}

// TestRunPassesTheEnvironmentAndDirectory covers the contract a script is written
// against: it runs in the directory it was given, with what it was told about the agent in
// the environment, and inherits the rest of the environment around it.
func TestRunPassesTheEnvironmentAndDirectory(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	script := filepath.Join(dir, "record")
	require.NoError(t, os.WriteFile(script, []byte(
		"#!/bin/sh\n{ printf '%s\\n' \"$WEAVE_AGENT\" \"$PWD\" \"$WEAVE_TEST_INHERITED\"; } > "+out+"\n",
	), 0o755))

	// Set on this process rather than passed in, so what the script sees of it is
	// evidence the environment around the run is inherited rather than replaced.
	t.Setenv("WEAVE_TEST_INHERITED", "from the panel")

	work := t.TempDir()
	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	require.NoError(t, Runner{}.Run(found[0], work, []string{`WEAVE_AGENT={"name":"auth"}`}))

	written := eventually(t, out)
	assert.Contains(t, written, `{"name":"auth"}`, "the payload reaches the script")
	assert.Contains(t, written, work, "the script runs in the directory it was given")
	assert.Contains(t, written, "from the panel", "the environment around it is inherited, not replaced")
}

// TestRunRefusesWhatItCannotStart covers the failures that would otherwise be
// indistinguishable from a keypress that did nothing at all.
func TestRunRefusesWhatItCannotStart(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		contents string
		mode     os.FileMode
		// wantIs is the error the refusal has to carry, nil when any failure will do.
		wantIs error
	}{
		{
			name:     "a file without the executable bit names the chmod that fixes it",
			file:     "unrunnable",
			contents: "#!/bin/sh\n",
			mode:     0o644,
			wantIs:   ErrNotExecutable,
		},
		{
			name:     "a shebang naming an interpreter that is not there fails to start",
			file:     "bad-shebang",
			contents: "#!/nope/sh\n",
			mode:     0o755,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(dir, tt.file), []byte(tt.contents), tt.mode))

			found, err := Discover(dir)
			require.NoError(t, err)
			require.Len(t, found, 1)

			err = Runner{}.Run(found[0], dir, nil)

			require.Error(t, err)
			if tt.wantIs != nil {
				assert.ErrorIs(t, err, tt.wantIs)
			}
		})
	}
}

// TestRunRefusesAMissingDirectory covers an agent outliving the worktree it ran in, which is
// ordinary once a worktree is cleaned up. The refusal has to name the directory: the exec failure
// underneath it reports no such file against the script, which is the one thing here that is
// fine.
func TestRunRefusesAMissingDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "open-pr"), 0o755)

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	gone := filepath.Join(t.TempDir(), "worktree-that-was-removed")

	err = Runner{}.Run(found[0], gone, nil)

	require.ErrorIs(t, err, ErrDirMissing)
	assert.Contains(t, err.Error(), gone, "the refusal names the directory, not the script")
}

// TestRunRefusesADirectoryThatIsAFile covers the same check meeting a path that exists and is
// not somewhere a process can be started, which fails at chdir the same way nothing there does.
func TestRunRefusesADirectoryThatIsAFile(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "open-pr"), 0o755)

	found, err := Discover(dir)
	require.NoError(t, err)
	require.Len(t, found, 1)

	notADir := filepath.Join(dir, "open-pr")

	assert.ErrorIs(t, Runner{}.Run(found[0], notADir, nil), ErrDirMissing)
}

// write puts an empty file at path with the given mode, standing in for a script whose
// contents no test reads.
func write(t *testing.T, path string, mode os.FileMode) {
	t.Helper()

	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), mode))
}

// eventually reads a file a started script is on its way to writing. Run returns as soon
// as the process exists rather than once it has finished, so the only thing a test can do
// is wait for what it left behind.
func eventually(t *testing.T, path string) string {
	t.Helper()

	var content []byte
	require.Eventually(t, func() bool {
		read, err := os.ReadFile(path)
		content = read

		return err == nil && len(read) > 0
	}, 5*time.Second, 10*time.Millisecond, "the script never wrote %s", path)

	return string(content)
}
