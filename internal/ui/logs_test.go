package ui

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logsModel builds an agents model whose log key has a readable log to show, and returns
// the path it points at.
func logsModel(t *testing.T, tmux *recordingTmux) (model, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "weave.log")
	require.NoError(t, os.WriteFile(path, []byte("listening on 127.0.0.1:8787\n"), 0o644))

	m := infoModel(tmux)
	m.logPath = path

	return m, path
}

// pressLogs sends the log key and returns the panel it left.
func pressLogs(t *testing.T, m model) model {
	t.Helper()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	panel, ok := next.(model)
	require.True(t, ok)

	return panel
}

// TestLogsReplaceTheStagedAgent covers the log key. The cursor is on a live agent, whose
// own pane is swapped into the stage, so showing the log has to put that pane back before
// anything is drawn over the slot it was sitting in.
func TestLogsReplaceTheStagedAgent(t *testing.T) {
	tmux := &recordingTmux{}
	m, path := logsModel(t, tmux)

	m.stageSelection()
	require.Equal(t, 1, tmux.swaps, "the live agent is staged to begin with")

	shown := pressLogs(t, m)

	assert.True(t, shown.tree.logsPinned)
	assert.Nil(t, shown.banner)
	assert.Equal(t, 2, tmux.swaps, "the agent's pane goes back to its own window")
	assert.Equal(t, []string{"less -R -X +G " + path}, tmux.respawns)
	assert.Empty(t, tmux.zooms, "the stage keeps its share of the width")
}

// TestLogsToggleBackToTheRow covers the second press. The key is the way out as well as
// the way in, so the stage goes back to the agent it was showing.
func TestLogsToggleBackToTheRow(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	m.stageSelection()
	require.Equal(t, 1, tmux.swaps, "the live agent is staged to begin with")

	hidden := pressLogs(t, pressLogs(t, m))

	assert.False(t, hidden.tree.logsPinned)
	assert.Equal(t, 3, tmux.swaps, "the agent's pane goes out for the log and comes back after it")
}

// TestLogsUnpinOnACursorMove covers moving off the row the log was opened from. The stage
// follows the cursor unless something is pinned over it, so moving is the other way back
// to the row, the way it is for info.
func TestLogsUnpinOnACursorMove(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	shown := pressLogs(t, m)
	require.True(t, shown.tree.logsPinned)

	moved, _ := shown.Update(tea.KeyMsg{Type: tea.KeyUp})
	panel, ok := moved.(model)
	require.True(t, ok)

	assert.False(t, panel.tree.logsPinned)
}

// TestLogsSurviveARefresh covers the pass that restages every second the panel is open.
// The stage compares what it is asked for against what it is already showing, so a log
// left up is not respawned under the person reading it.
func TestLogsSurviveARefresh(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	shown := pressLogs(t, m)
	require.Len(t, tmux.respawns, 1)

	shown.stageSelection()
	shown.stageSelection()

	assert.Len(t, tmux.respawns, 1, "the pager is left alone")
}

// TestLogsWithNoLogFile covers the key pressed before the service has ever written
// anything. less given a path it cannot open exits at once and takes the stage pane down
// with it, which reads as a key that did nothing, so the panel says so itself instead.
func TestLogsWithNoLogFile(t *testing.T) {
	tmux := &recordingTmux{}
	m, path := logsModel(t, tmux)
	m.logPath = filepath.Join(filepath.Dir(path), "absent.log")

	shown := pressLogs(t, m)

	assert.NotNil(t, shown.banner)
	assert.False(t, shown.tree.logsPinned)
	assert.Empty(t, tmux.respawns)
}

// TestLogsShowOnAnEmptyList covers the log asked for with nothing to list, which is one of
// the moments it is worth the most: an empty panel is what a service that cannot answer
// looks like, and the reason why is in the file.
func TestLogsShowOnAnEmptyList(t *testing.T) {
	tmux := &recordingTmux{}
	m, path := logsModel(t, tmux)
	m.tree = treeState{}

	shown := pressLogs(t, m)

	assert.True(t, shown.tree.logsPinned)
	assert.Equal(t, []string{"less -R -X +G " + path}, tmux.respawns)
}
