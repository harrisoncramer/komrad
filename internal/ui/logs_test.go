package ui

import (
	"os"
	"path/filepath"
	"testing"

	"weave/config"

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

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	panel, ok := next.(model)
	require.True(t, ok)

	return panel
}

// TestLogsOpenBelowTheStagedAgent covers the log key. The cursor is on a live agent, whose
// own pane is swapped into the stage, and the log opens under it rather than over it, so
// the agent stays on screen while the log is read.
func TestLogsOpenBelowTheStagedAgent(t *testing.T) {
	tmux := &recordingTmux{}
	m, path := logsModel(t, tmux)

	m.stageSelection()
	require.Equal(t, 1, tmux.swaps, "the live agent is staged to begin with")
	require.Empty(t, tmux.respawns, "nothing is open below it")

	shown := pressLogs(t, m)

	assert.Equal(t, infoLogs, shown.info.kind)
	assert.Nil(t, shown.banner)
	assert.Equal(t, 1, tmux.swaps, "the agent's pane is left where it is")
	require.Len(t, tmux.respawns, 1)
	assert.Equal(t, "%2", tmux.respawnPanes[0], "the log is paged into the pane below")
	assert.Contains(t, tmux.respawns[0], "export WEAVE_LOG='"+path+"'")
	assert.Contains(t, tmux.respawns[0], config.DefaultLogViewer)
}

// TestLogsToggleClosesThePane covers the second press. The key is the way out as well as the
// way in, and the log is the only thing that pane holds, so the way out takes the pane with it.
func TestLogsToggleClosesThePane(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	m.stageSelection()

	hidden := pressLogs(t, pressLogs(t, m))

	assert.Equal(t, infoNothing, hidden.info.kind)
	assert.Contains(t, tmux.killed, "%2", "the log's own pane goes with it")
}

// TestLogsSurviveACursorMove covers moving off the row the log was opened from. The log is
// about the service rather than the row, so it stays where it is and the stage above it
// follows the cursor on its own.
func TestLogsSurviveACursorMove(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	shown := pressLogs(t, m)
	require.Equal(t, infoLogs, shown.info.kind)

	moved, _ := shown.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	panel, ok := moved.(model)
	require.True(t, ok)

	assert.Equal(t, infoLogs, panel.info.kind)
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
	assert.Equal(t, infoNothing, shown.info.kind)
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

	assert.Equal(t, infoLogs, shown.info.kind)
	require.Len(t, tmux.respawns, 1)
	assert.Contains(t, tmux.respawns[0], "export WEAVE_LOG='"+path+"'")
}

// TestLogsOpenFromEveryScreen covers the key on the screens that list something other than
// agents. The log is about the service, which is as likely to be doubted from the schedules
// or the archive as from the agents list.
func TestLogsOpenFromEveryScreen(t *testing.T) {
	screens := map[string]screen{
		"schedules": screenSchedules,
		"archive":   screenArchive,
		"actions":   screenActions,
	}

	for name, from := range screens {
		t.Run(name, func(t *testing.T) {
			tmux := &recordingTmux{}
			m, path := logsModel(t, tmux)
			m.screen = from

			shown := pressLogs(t, m)

			assert.Equal(t, infoLogs, shown.info.kind)
			assert.Equal(t, from, shown.screen, "the key opens the log without leaving the screen")
			require.Len(t, tmux.respawns, 1)
			assert.Contains(t, tmux.respawns[0], "export WEAVE_LOG='"+path+"'")
		})
	}
}

// TestLogsTypeIntoAnOpenQuery covers the key while the search screen's query line has it,
// where a key bound to something has to type rather than act.
func TestLogsTypeIntoAnOpenQuery(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)
	m.screen = screenSearch
	m.search.typing = true

	shown := pressLogs(t, m)

	assert.Equal(t, infoNothing, shown.info.kind)
	assert.Equal(t, "l", shown.search.query)
	assert.Empty(t, tmux.respawns)
}

// TestLogArgv covers the command the log key runs, which is the user's rather than the
// panel's. The command's inputs are exported into its environment rather than substituted
// into its text, so a tool's own placeholders stay its own: fzf's "{}" means the selected
// line, and a panel that rewrote it before fzf saw it would break half of fzf's manual.
func TestLogArgv(t *testing.T) {
	tests := []struct {
		name   string
		viewer string
		path   string
		want   string
	}{
		{
			name: "nothing configured falls back to a pager on the file",
			path: "/tmp/weave.log",
			want: config.DefaultLogViewer,
		},
		{
			name:   "a pipeline is left exactly as it was written",
			viewer: `jq -c . "$WEAVE_LOG" | fzf --ansi --preview 'echo {}'`,
			path:   "/tmp/weave.log",
			want:   `jq -c . "$WEAVE_LOG" | fzf --ansi --preview 'echo {}'`,
		},
		{
			name:   "a viewer that is only whitespace is no viewer at all",
			viewer: "   ",
			path:   "/tmp/weave.log",
			want:   config.DefaultLogViewer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argv := logArgv(tt.viewer, tt.path)

			require.Len(t, argv, 3)
			assert.Equal(t, []string{"sh", "-c"}, argv[:2], "a pipeline needs a shell to read it")
			assert.Contains(t, argv[2], tt.want)
			assert.Contains(t, argv[2], "export WEAVE_LOG='"+tt.path+"'; ")
		})
	}
}

// TestLogArgvQuotesTheEnvironment covers a path the shell would otherwise read as two
// words, or as the end of a quoted string.
func TestLogArgvQuotesTheEnvironment(t *testing.T) {
	argv := logArgv("", "/Users/a b/it's.log")

	assert.Contains(t, argv[2], `export WEAVE_LOG='/Users/a b/it'\''s.log'`)
}

// TestLogsUseTheConfiguredViewer covers the key itself running what the config named,
// rather than the panel's own default reaching the pane whatever the user asked for.
func TestLogsUseTheConfiguredViewer(t *testing.T) {
	tmux := &recordingTmux{}
	m, path := logsModel(t, tmux)
	m.logViewer = `jq -c . "$WEAVE_LOG" | fzf --ansi --tac`

	shown := pressLogs(t, m)

	require.Equal(t, infoLogs, shown.info.kind)
	require.Len(t, tmux.respawns, 1)
	assert.Contains(t, tmux.respawns[0], `jq -c . "$WEAVE_LOG" | fzf --ansi --tac`)
	assert.Contains(t, tmux.respawns[0], "export WEAVE_LOG='"+path+"'")
}
