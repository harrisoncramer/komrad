package ui

import (
	"os"
	"path/filepath"
	"testing"

	"weave/config"
	agentstore "weave/internal/store/agent"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func logsModel(t *testing.T, tmux *recordingTmux) (model, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "weave.log")
	require.NoError(t, os.WriteFile(path, []byte("listening on 127.0.0.1:8787\n"), 0o644))

	m := infoModel(tmux)
	m.logPath = path
	m.actionLogsDir = t.TempDir()
	m.listing.agents[0].agent.ID = "a1"

	return m, path
}

func pressLogs(t *testing.T, m model) model {
	t.Helper()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	panel, ok := next.(model)
	require.True(t, ok)

	return panel
}

// TestCycleLogsWalksTheStops pins the order the logs key moves through, and that it pages
// each one into the pane below rather than disturbing the agent's own.
func TestCycleLogsWalksTheStops(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	m.stageSelection()
	require.Equal(t, 1, tmux.swaps, "the live agent is staged to begin with")

	first := pressLogs(t, m)
	assert.Equal(t, infoSetupLog, first.info.kind)
	assert.Equal(t, 1, tmux.swaps, "the agent's pane is left where it is")
	require.Len(t, tmux.respawns, 1)
	assert.Equal(t, "%2", tmux.respawnPanes[0], "the log is paged into the pane below")

	second := pressLogs(t, first)
	assert.Equal(t, infoTeardownLog, second.info.kind)

	third := pressLogs(t, second)
	assert.Equal(t, infoAgentLog, third.info.kind)

	fourth := pressLogs(t, third)
	assert.Equal(t, infoNothing, fourth.info.kind)
	assert.Contains(t, tmux.killed, "%2", "the log's own pane goes with it")

	fifth := pressLogs(t, fourth)
	assert.Equal(t, infoSetupLog, fifth.info.kind, "cycling past closed reopens at the first stop")
	assert.Len(t, tmux.respawns, 4, "one respawn per stop already visited, plus the reopened one")
}

// TestCycleLogsDoesNothingWithoutASelectedAgent pins that the key is inert when there is no
// agent to show a log for.
func TestCycleLogsDoesNothingWithoutASelectedAgent(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)
	m.tree = treeState{}

	shown := pressLogs(t, m)

	assert.Equal(t, infoNothing, shown.info.kind)
	assert.Empty(t, tmux.respawns)
}

// TestCycleLogsBlockedWhileASetupIsStreaming pins that a setup still writing to the pane is
// left alone rather than paged over.
func TestCycleLogsBlockedWhileASetupIsStreaming(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)
	m.info.kind = infoSetup
	m.setupPending = true

	shown := pressLogs(t, m)

	assert.Equal(t, infoSetup, shown.info.kind, "the live pane is left alone rather than hijacked")
	assert.Empty(t, tmux.respawns)
}

// TestCycleLogsWorksAfterASetupHasResolved pins that a finished setup stops blocking the key,
// even while its pane is still up.
func TestCycleLogsWorksAfterASetupHasResolved(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)
	m.info.kind = infoSetup
	m.setupPending = false

	shown := pressLogs(t, m)

	assert.Equal(t, infoSetupLog, shown.info.kind, "a resolved create no longer blocks the key, even though the crash pane is still up")
}

// TestCycleLogsSurvivesACursorMove pins that moving the cursor leaves the staged log where it
// is rather than resetting the stop.
func TestCycleLogsSurvivesACursorMove(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	shown := pressLogs(t, m)
	require.Equal(t, infoSetupLog, shown.info.kind)

	moved, _ := shown.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	panel, ok := moved.(model)
	require.True(t, ok)

	assert.Equal(t, infoSetupLog, panel.info.kind)
}

// TestCycleLogsTypeIntoAnOpenQuery pins that the key is a character while a search is being
// typed, so the query takes it instead of the stage.
func TestCycleLogsTypeIntoAnOpenQuery(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)
	m.screen = screenSearch
	m.search.typing = true

	shown := pressLogs(t, m)

	assert.Equal(t, infoNothing, shown.info.kind)
	assert.Equal(t, "l", shown.search.query)
	assert.Empty(t, tmux.respawns)
}

// TestActionLogContentPlaceholdersWhenEmpty pins the placeholder shown before an action has
// written any log at all.
func TestActionLogContentPlaceholdersWhenEmpty(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	content := m.actionLogContent(config.ActionLogSetup)

	assert.Contains(t, content.Key, "no setup log yet")
}

// TestActionLogContentPagesTheDurableFile pins that an action log on disk is paged through
// the configured viewer with the path exported to it.
func TestActionLogContentPagesTheDurableFile(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	path := config.ActionLogPath(m.actionLogsDir, "a1", config.ActionLogSetup)
	require.NoError(t, os.WriteFile(path, []byte("cloning worktree\n"), 0o644))

	content := m.actionLogContent(config.ActionLogSetup)
	argv, ok := content.Render(layout.Target{})

	require.True(t, ok)
	assert.Contains(t, argv[2], "export WEAVE_LOG='"+path+"'")
	assert.Contains(t, argv[2], config.DefaultProcessViewer)
}

// TestActionLogContentWithNoAgentSelected pins the placeholder shown when there is no agent
// whose action log could be read.
func TestActionLogContentWithNoAgentSelected(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)
	m.tree = treeState{}

	content := m.actionLogContent(config.ActionLogSetup)

	assert.Contains(t, content.Key, "select an agent")
}

// TestAgentLogContentExportsTheAgentID pins that the agent log viewer is handed both the log
// path and the agent's id, which is what a filtering viewer needs.
func TestAgentLogContentExportsTheAgentID(t *testing.T) {
	tmux := &recordingTmux{}
	m, path := logsModel(t, tmux)

	content := m.agentLogContent()
	argv, ok := content.Render(layout.Target{})

	require.True(t, ok)
	assert.Contains(t, argv[2], "export WEAVE_LOG='"+path+"'")
	assert.Contains(t, argv[2], "export WEAVE_AGENT_ID='a1'")
}

// TestLogArgv pins what the viewer command becomes for each configuration, from nothing set
// at all to a pipeline that must be left exactly as written.
func TestLogArgv(t *testing.T) {
	tests := []struct {
		name    string
		viewer  string
		path    string
		agentID string
		want    string
	}{
		{
			name: "nothing configured falls back to a pager on the file",
			path: "/tmp/weave.log",
			want: config.DefaultLogViewer,
		},
		{
			name:    "a pipeline is left exactly as it was written",
			viewer:  `jq -c . "$WEAVE_LOG" | fzf --ansi --preview 'echo {}'`,
			path:    "/tmp/weave.log",
			agentID: "a1",
			want:    `jq -c . "$WEAVE_LOG" | fzf --ansi --preview 'echo {}'`,
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
			argv := logArgv(tt.viewer, tt.path, tt.agentID)

			require.Len(t, argv, 3)
			assert.Equal(t, []string{"sh", "-c"}, argv[:2], "a pipeline needs a shell to read it")
			assert.Contains(t, argv[2], tt.want)
			assert.Contains(t, argv[2], "export WEAVE_LOG='"+tt.path+"'; ")
			if tt.agentID != "" {
				assert.Contains(t, argv[2], "export WEAVE_AGENT_ID='"+tt.agentID+"'; ")
			} else {
				assert.NotContains(t, argv[2], "export WEAVE_AGENT_ID")
			}
		})
	}
}

// TestLogArgvQuotesTheEnvironment pins that a path holding spaces and quotes survives being
// exported into the shell the viewer runs in.
func TestLogArgvQuotesTheEnvironment(t *testing.T) {
	argv := logArgv("", "/Users/a b/it's.log", "")

	assert.Contains(t, argv[2], `export WEAVE_LOG='/Users/a b/it'\''s.log'`)
}

// TestProcessLogArgvFallsBackToTheDefault pins the viewer a process log gets when none is
// configured, and that no agent id is exported to it.
func TestProcessLogArgvFallsBackToTheDefault(t *testing.T) {
	argv := processLogArgv("", "/tmp/setup.log")

	require.Len(t, argv, 3)
	assert.Contains(t, argv[2], config.DefaultProcessViewer)
	assert.Contains(t, argv[2], "export WEAVE_LOG='/tmp/setup.log'; ")
	assert.NotContains(t, argv[2], "WEAVE_AGENT_ID")
}

// TestFailedPlaceholderPointsAtTheLogWhenThereIsOne covers the two ways a failed agent's
// placeholder can go: when its action left a durable log, the placeholder just points at
// it, since repeating the reason there too would be the same failure shown twice; when there
// is no log to point at — a failure that never reached an action at all, like a tmux refusal
// — the reason is the only place left to read it, so it is shown inline instead.
func TestFailedPlaceholderPointsAtTheLogWhenThereIsOne(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	agent := agentstore.Agent{
		ID:    "a1",
		State: agentstore.StateFailedToCreate,
		Metadata: []agentstore.Metadata{
			{Key: "error", Value: "failed to open a window: tmux refused"},
		},
	}

	path := config.ActionLogPath(m.actionLogsDir, agent.ID, config.ActionLogSetup)
	require.NoError(t, os.WriteFile(path, []byte("cloning worktree\n"), 0o644))

	withLog := m.failedPlaceholder(agent)
	assert.Contains(t, withLog, `Press "l"`)
	assert.NotContains(t, withLog, "tmux refused", "the log already says why, so the reason is not repeated here")

	require.NoError(t, os.Remove(path))

	withoutLog := m.failedPlaceholder(agent)
	assert.Contains(t, withoutLog, "tmux refused", "nothing else is left to say why, so the reason falls back to here")
}

// TestLogKeysFollowTheArchiveCursor pins that the archive's own cursor picks which teardown
// log is paged, not whatever the agents list was last left pointing at.
func TestLogKeysFollowTheArchiveCursor(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)
	m.archive = archiveState{
		agents:    []agentstore.Agent{{ID: "a2", Name: "api-refactor", WorkflowID: "w1", State: agentstore.StateFailedToCleanup}},
		rowCursor: rowCursor{cursor: 1},
	}

	for _, id := range []string{"a1", "a2"} {
		path := config.ActionLogPath(m.actionLogsDir, id, config.ActionLogTeardown)
		require.NoError(t, os.WriteFile(path, []byte("boom\n"), 0o644))
	}

	assert.Contains(t, m.actionLogContent(config.ActionLogTeardown).Key, "a1", "the agents screen reads its own cursor")

	m.screen = screenArchive

	assert.Contains(t, m.actionLogContent(config.ActionLogTeardown).Key, "a2")

	argv, ok := m.agentLogContent().Render(layout.Target{})
	require.True(t, ok)
	require.Len(t, argv, 3)
	assert.Contains(t, argv[2], "export WEAVE_AGENT_ID='a2'; ", "the panel's own log is filtered to the archived agent")
}
