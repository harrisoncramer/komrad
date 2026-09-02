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

	got := LoadEnvironment(connection)

	assert.Equal(t, connection.Addr, got.Addr)
	assert.Equal(t, connection.CacheDir, got.CacheDir)
	assert.Equal(t, "nvim", got.Editor)
	assert.Equal(t, "/tmp/tmux,1,0", got.Tmux)
	assert.Equal(t, "%4", got.TmuxPane)
	assert.Equal(t, "agent-1", got.AgentID)
	assert.Equal(t, "session-1", got.SessionID)
	assert.Equal(t, "/tmp/weave-cache/weave.log", got.LogPath())
}

// TestLoadDefaultsEditor pins the editor used when the process names none.
func TestLoadDefaultsEditor(t *testing.T) {
	t.Setenv("EDITOR", "")

	assert.Equal(t, "vi", LoadEnvironment(Environment{}).Editor)
}

// TestLayerPathsUsesPlatformPathList pins that several config paths are separated the way the
// platform separates a path list.
func TestLayerPathsUsesPlatformPathList(t *testing.T) {
	want := []string{"/one.toml", "/two.toml"}
	t.Setenv("WEAVE_CONFIG", strings.Join(want, string(filepath.ListSeparator)))

	assert.Equal(t, want, LayerPaths())
}

// TestListActionLogRuns pins the order the panel reads runs in and what it leaves out, since
// the number is parsed rather than sorted on: a run past the ninth hundred is wider than the
// ones before it and would otherwise sort under them.
func TestListActionLogRuns(t *testing.T) {
	dir := t.TempDir()
	logDir := ActionLogDir(dir, "a1")
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	for _, name := range []string{
		ActionLogName(999, TriggerProvision, "make-worktree"),
		ActionLogName(1000, TriggerManual, "open-pr"),
		ActionLogName(1, TriggerProvision, "make-worktree"),
		ActionLogName(500, HookPreLaunch, "install-deps"),
		"notes.txt",
		"garbled.log",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(logDir, name), []byte("out\n"), 0o644))
	}

	runs := ListActionLogRuns(dir, "a1")

	require.Len(t, runs, 4, "only the numbered logs are runs")
	assert.Equal(t, 1000, runs[0].Number, "the newest run is first")
	assert.Equal(t, TriggerManual, runs[0].Trigger)
	assert.Equal(t, "open-pr", runs[0].Action)
	assert.Equal(t, 999, runs[1].Number)
	assert.Equal(t, HookPreLaunch, runs[2].Trigger, "a moment whose own name holds a hyphen is read whole")
	assert.Equal(t, "install-deps", runs[2].Action)
	assert.Equal(t, 1, runs[3].Number)
	assert.Equal(t, filepath.Join(logDir, ActionLogName(1000, TriggerManual, "open-pr")), runs[0].Path)
}

// TestListActionLogRunsWithNothingToRead pins the empty answer, since an agent that has run
// no action has no directory at all and that is not a fault.
func TestListActionLogRunsWithNothingToRead(t *testing.T) {
	assert.Empty(t, ListActionLogRuns(t.TempDir(), "a1"))
	assert.Empty(t, ListActionLogRuns("", "a1"))
	assert.Empty(t, ListActionLogRuns(t.TempDir(), ""))
}
