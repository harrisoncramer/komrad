package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadAddsProcessEnvironment pins which process variables are read into the environment
// alongside the settings the connection carries.
func TestLoadAddsProcessEnvironment(t *testing.T) {
	t.Setenv("EDITOR", "nvim")
	t.Setenv("TMUX", "/tmp/tmux,1,0")
	t.Setenv("TMUX_PANE", "%4")
	t.Setenv("WEAVE_AGENT_ID", "agent-1")
	t.Setenv("WEAVE_SESSION_ID", "session-1")

	connection := Environment{Addr: "127.0.0.1:9999", CacheDir: "/tmp/weave-cache"}

	got, err := LoadEnvironment(connection)
	require.NoError(t, err)

	assert.Equal(t, connection.Addr, got.Addr)
	assert.Equal(t, connection.CacheDir, got.CacheDir)
	assert.Equal(t, "nvim", got.Editor)
	assert.Equal(t, "/tmp/tmux,1,0", got.Tmux)
	assert.Equal(t, "%4", got.TmuxPane)
	assert.Equal(t, "agent-1", got.AgentID)
	assert.Equal(t, "session-1", got.SessionID)
	assert.Equal(t, "/tmp/weave-cache/weave.log", got.LogPath())
	assert.NotEmpty(t, got.StateDir, "the state directory is filled in when the caller names none")
}

// TestLoadKeepsAStateDirItWasGiven covers a test instance pointing the service at a state
// directory of its own rather than the user's.
func TestLoadKeepsAStateDirItWasGiven(t *testing.T) {
	t.Setenv("EDITOR", "")

	got, err := LoadEnvironment(Environment{StateDir: "/tmp/weave-state"})
	require.NoError(t, err)
	assert.Equal(t, "/tmp/weave-state", got.StateDir)
}

// TestLoadDefaultsEditor pins the editor used when the process names none.
func TestLoadDefaultsEditor(t *testing.T) {
	t.Setenv("EDITOR", "")

	got, err := LoadEnvironment(Environment{})
	require.NoError(t, err)
	assert.Equal(t, "vi", got.Editor)
}

// TestLayerPathsUsesPlatformPathList pins that several config paths are separated the way the
// platform separates a path list.
func TestLayerPathsUsesPlatformPathList(t *testing.T) {
	want := []string{"/one.toml", "/two.toml"}
	t.Setenv("WEAVE_CONFIG", strings.Join(want, string(filepath.ListSeparator)))

	assert.Equal(t, want, LayerPaths())
}

// TestListRunLogs pins the order the panel reads runs in and what it leaves out, with the number
// parsed rather than sorted on.
func TestListRunLogs(t *testing.T) {
	dir := t.TempDir()
	logDir := RunLogDir(dir, "a1")
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	for _, name := range []string{
		RunLogName(999, TriggerProvision, "make-worktree"),
		RunLogName(1000, TriggerDeprovision, "_worktree_cleanup"),
		RunLogName(1, TriggerProvision, "make-worktree"),
		RunLogName(500, HookPreLaunch, "install-deps"),
		"notes.txt",
		"garbled.log",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(logDir, name), []byte("out\n"), 0o644))
	}

	runs := ListRunLogs(dir, "a1")

	require.Len(t, runs, 4, "only the numbered logs are runs")
	assert.Equal(t, 1000, runs[0].Number, "the newest run is first")
	assert.Equal(t, TriggerDeprovision, runs[0].Trigger)
	assert.Equal(t, "_worktree_cleanup", runs[0].Script)
	assert.Equal(t, 999, runs[1].Number)
	assert.Equal(t, HookPreLaunch, runs[2].Trigger, "a moment whose own name holds a hyphen is read whole")
	assert.Equal(t, "install-deps", runs[2].Script)
	assert.Equal(t, 1, runs[3].Number)
	assert.Equal(t, filepath.Join(logDir, RunLogName(1000, TriggerDeprovision, "_worktree_cleanup")), runs[0].Path)
}

// TestListRunLogsWithNothingToRead pins the empty answer an agent that has run no script gets.
func TestListRunLogsWithNothingToRead(t *testing.T) {
	assert.Empty(t, ListRunLogs(t.TempDir(), "a1"))
	assert.Empty(t, ListRunLogs("", "a1"))
	assert.Empty(t, ListRunLogs(t.TempDir(), ""))
}
