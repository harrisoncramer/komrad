package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	eventv1 "komrad/proto/gen/event/v1"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadAddsProcessEnvironment pins which process variables are read into the environment
// alongside the settings the connection carries.
func TestLoadAddsProcessEnvironment(t *testing.T) {
	t.Setenv("EDITOR", "nvim")
	t.Setenv("TMUX", "/tmp/tmux,1,0")
	t.Setenv("TMUX_PANE", "%4")
	agentID := uuid.New()
	t.Setenv("KOMRAD_AGENT_ID", agentID.String())
	t.Setenv("KOMRAD_SESSION_ID", "session-1")

	connection := Environment{Addr: "127.0.0.1:9999", CacheDir: "/tmp/komrad-cache"}

	got, err := LoadEnvironment(connection)
	require.NoError(t, err)

	assert.Equal(t, connection.Addr, got.Addr)
	assert.Equal(t, connection.CacheDir, got.CacheDir)
	assert.Equal(t, "nvim", got.Editor)
	assert.Equal(t, "/tmp/tmux,1,0", got.Tmux)
	assert.Equal(t, "%4", got.TmuxPane)
	assert.Equal(t, agentID, got.AgentID)
	assert.Equal(t, "session-1", got.SessionID)
	assert.Equal(t, "/tmp/komrad-cache/komrad.log", got.LogPath())
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
	t.Setenv("KOMRAD_CONFIG", strings.Join(want, string(filepath.ListSeparator)))

	assert.Equal(t, want, LayerPaths())
}

// TestListRunLogs pins the order the panel reads runs in and what it leaves out, with the number
// parsed rather than sorted on.
func TestListRunLogs(t *testing.T) {
	dir := t.TempDir()
	logDir := RunLogDir(dir, uuid.MustParse("00000000-0000-7000-8000-f29bc91bbdab"))
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	for _, name := range []string{
		RunLogName(999, eventv1.Moment_MOMENT_AGENT_PROVISION, "make-worktree"),
		RunLogName(1000, eventv1.Moment_MOMENT_AGENT_DEPROVISION, "_worktree_cleanup"),
		RunLogName(1, eventv1.Moment_MOMENT_AGENT_PROVISION, "make-worktree"),
		RunLogName(500, eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, "install-deps"),
		RunLogName(700, eventv1.Moment_MOMENT_TASK_POST_ARCHIVE, "shrink"),
		"notes.txt",
		"garbled.log",
	} {
		require.NoError(t, os.WriteFile(filepath.Join(logDir, name), []byte("out\n"), 0o644))
	}

	runs := ListRunLogs(dir, uuid.MustParse("00000000-0000-7000-8000-f29bc91bbdab"))

	require.Len(t, runs, 5, "only the numbered logs are runs")
	assert.Equal(t, 1000, runs[0].Number, "the newest run is first")
	assert.Equal(t, eventv1.Moment_MOMENT_AGENT_DEPROVISION, runs[0].Trigger)
	assert.Equal(t, "_worktree_cleanup", runs[0].Script)
	assert.Equal(t, 999, runs[1].Number)
	assert.Equal(t, eventv1.Moment_MOMENT_TASK_POST_ARCHIVE, runs[2].Trigger, "a task's moment is read whole")
	assert.Equal(t, "shrink", runs[2].Script)
	assert.Equal(t, eventv1.Moment_MOMENT_AGENT_PRE_LAUNCH, runs[3].Trigger, "a moment whose own name holds a hyphen is read whole")
	assert.Equal(t, "install-deps", runs[3].Script)
	assert.Equal(t, 1, runs[4].Number)
	assert.Equal(t, filepath.Join(logDir, RunLogName(1000, eventv1.Moment_MOMENT_AGENT_DEPROVISION, "_worktree_cleanup")), runs[0].Path)
}

// TestListRunLogsWithNothingToRead pins the empty answer an agent that has run no script gets.
func TestListRunLogsWithNothingToRead(t *testing.T) {
	assert.Empty(t, ListRunLogs(t.TempDir(), uuid.MustParse("00000000-0000-7000-8000-f29bc91bbdab")))
	assert.Empty(t, ListRunLogs("", uuid.MustParse("00000000-0000-7000-8000-f29bc91bbdab")))
	assert.Empty(t, ListRunLogs(t.TempDir(), uuid.Nil))
}
