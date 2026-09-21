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
	m.runLogsDir = t.TempDir()
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

func pressOlderLog(t *testing.T, m model) model {
	t.Helper()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'L'}})
	panel, ok := next.(model)
	require.True(t, ok)

	return panel
}

// writeRun leaves one run of a script on disk for an agent, which is what the panel reads
// instead of a file per kind.
func writeRun(t *testing.T, dir, agentID string, number int, trigger, script string) string {
	t.Helper()

	logDir := config.RunLogDir(dir, agentID)
	require.NoError(t, os.MkdirAll(logDir, 0o755))

	path := filepath.Join(logDir, config.RunLogName(number, trigger, script))
	require.NoError(t, os.WriteFile(path, []byte("cloning worktree\n"), 0o644))

	return path
}

// TestCycleLogsWalksTheStops pins the order the logs key moves through, and that it pages
// each one into the pane below rather than disturbing the agent's own.
func TestCycleLogsWalksTheStops(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	m.stageSelection()
	require.Equal(t, 1, tmux.swaps, "the live agent is staged to begin with")

	first := pressLogs(t, m)
	assert.Equal(t, infoRunLog, first.info.kind)
	assert.Equal(t, 1, tmux.swaps, "the agent's pane is left where it is")
	require.Len(t, tmux.respawns, 1)
	assert.Equal(t, "%2", tmux.respawnPanes[0], "the log is paged into the pane below")

	second := pressLogs(t, first)
	assert.Equal(t, infoAgentLog, second.info.kind)

	third := pressLogs(t, second)
	assert.Equal(t, infoNothing, third.info.kind)
	assert.Contains(t, tmux.killed, "%2", "the log's own pane goes with it")

	fourth := pressLogs(t, third)
	assert.Equal(t, infoRunLog, fourth.info.kind, "cycling past closed reopens at the first stop")
	assert.Len(t, tmux.respawns, 3, "one respawn per stop already visited, plus the reopened one")
}

// TestOlderLogStepsBackThroughTheRuns pins the way back through an agent's runs, which is
// what one key onto the most recent one needs to be usable at all.
func TestOlderLogStepsBackThroughTheRuns(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	first := writeRun(t, m.runLogsDir, "a1", 1, config.TriggerProvision, "make-worktree")
	second := writeRun(t, m.runLogsDir, "a1", 2, config.TriggerResume, "make-worktree")

	shown := pressLogs(t, m)
	require.Equal(t, infoRunLog, shown.info.kind)
	assert.Contains(t, shown.runLogContent().Key, second, "the pane opens on the most recent run")

	stepped := pressOlderLog(t, shown)
	assert.Equal(t, 1, stepped.info.run)
	assert.Contains(t, stepped.runLogContent().Key, first, "and steps back to the one before it")

	wrapped := pressOlderLog(t, stepped)
	assert.Equal(t, 0, wrapped.info.run, "stepping past the oldest wraps round")
	assert.Contains(t, wrapped.runLogContent().Key, second)
}

// TestOlderLogIsInertWithoutARunToStepTo pins the key doing nothing when there is nowhere to
// step.
func TestOlderLogIsInertWithoutARunToStepTo(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	writeRun(t, m.runLogsDir, "a1", 1, config.TriggerProvision, "make-worktree")

	shown := pressLogs(t, m)
	stepped := pressOlderLog(t, shown)

	assert.Equal(t, 0, stepped.info.run, "one run has nothing before it")

	closed := pressOlderLog(t, m)
	assert.Equal(t, infoNothing, closed.info.kind, "and the key opens nothing of its own")
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

// TestCycleLogsSurvivesACursorMove pins that moving the cursor leaves the staged log where it
// is rather than resetting the stop.
func TestCycleLogsSurvivesACursorMove(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	shown := pressLogs(t, m)
	require.Equal(t, infoRunLog, shown.info.kind)

	moved, _ := shown.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	panel, ok := moved.(model)
	require.True(t, ok)

	assert.Equal(t, infoRunLog, panel.info.kind)
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

// TestRunLogContentPlaceholdersWhenEmpty pins the placeholder shown before a script has
// written any log at all.
func TestRunLogContentPlaceholdersWhenEmpty(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	content := m.runLogContent()

	assert.Contains(t, content.Key, "no script has run")
}

// TestRunLogContentPagesTheDurableFile pins that a script log on disk is paged through
// the configured viewer with the path exported to it.
func TestRunLogContentPagesTheDurableFile(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	path := writeRun(t, m.runLogsDir, "a1", 1, config.TriggerProvision, "make-worktree")

	content := m.runLogContent()
	argv, ok := content.Render(layout.Target{})

	require.True(t, ok)
	assert.Contains(t, argv[2], "export WEAVE_LOG='"+path+"'")
	assert.Contains(t, argv[2], config.DefaultProcessViewer)
}

// TestRunLogContentWithNoAgentSelected pins the placeholder shown when there is no agent
// whose script log could be read.
func TestRunLogContentWithNoAgentSelected(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)
	m.tree = treeState{}

	content := m.runLogContent()

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
// placeholder can go: at the log when a script left one, and at the reason inline when no
// script ever ran.
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

	path := writeRun(t, m.runLogsDir, agent.ID, 1, config.TriggerProvision, "make-worktree")

	withLog := m.failedPlaceholder(agent)
	assert.Contains(t, withLog, `Press "l"`)
	assert.NotContains(t, withLog, "tmux refused", "the log already says why, so the reason is not repeated here")

	require.NoError(t, os.Remove(path))

	withoutLog := m.failedPlaceholder(agent)
	assert.Contains(t, withoutLog, "tmux refused", "nothing else is left to say why, so the reason falls back to here")
}

// TestFailedPlaceholderPointsAtTheHookThatFailedTheCreate pins the placeholder following the
// run that actually failed.
func TestFailedPlaceholderPointsAtTheHookThatFailedTheCreate(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	agent := agentstore.Agent{
		ID:       "a1",
		State:    agentstore.StateFailedToCreate,
		Metadata: []agentstore.Metadata{{Key: "error", Value: "install-deps: mise refused"}},
	}

	writeRun(t, m.runLogsDir, agent.ID, 1, config.TriggerProvision, "make-worktree")
	writeRun(t, m.runLogsDir, agent.ID, 2, config.HookPreLaunch, "install-deps")

	assert.Contains(t, m.failedPlaceholder(agent), "run 002", "the hook's run is the one that failed")
}

// TestFailedPlaceholderPointsAtTheHookThatFailedTheDelete pins the same thing on the way out,
// where the deprovisioner never ran and left no run of its own.
func TestFailedPlaceholderPointsAtTheHookThatFailedTheDelete(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	agent := agentstore.Agent{
		ID:       "a1",
		State:    agentstore.StateFailedToCleanup,
		Metadata: []agentstore.Metadata{{Key: "error", Value: "take-the-stack-down: docker refused"}},
	}

	writeRun(t, m.runLogsDir, agent.ID, 1, config.TriggerProvision, "make-worktree")
	writeRun(t, m.runLogsDir, agent.ID, 2, config.HookPreTeardown, "take-the-stack-down")

	drawn := m.failedPlaceholder(agent)
	assert.Contains(t, drawn, "run 002", "the hook's run is the one that failed")
	assert.NotContains(t, drawn, "docker refused", "the log says why, so the reason is not repeated here")
}

// TestFailedPlaceholderSkipsARunWithNothingInIt pins a claimed but empty run being passed
// over.
func TestFailedPlaceholderSkipsARunWithNothingInIt(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	agent := agentstore.Agent{
		ID:       "a1",
		State:    agentstore.StateFailedToCreate,
		Metadata: []agentstore.Metadata{{Key: "error", Value: "tmux refused the window"}},
	}

	empty := writeRun(t, m.runLogsDir, agent.ID, 1, config.TriggerProvision, "make-worktree")
	require.NoError(t, os.Truncate(empty, 0))

	assert.Contains(t, m.failedPlaceholder(agent), "tmux refused", "with nothing to read, the reason is what is left")
}

// TestLogKeysAreInertInTheArchive pins that the archive draws its own two columns and none of
// the keys that open a pane beside the panel reach it.
func TestLogKeysAreInertInTheArchive(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)
	writeRun(t, m.runLogsDir, "a1", 1, config.TriggerDeprovision, "drop-worktree")
	m.screen = screenArchive

	pressed := pressLogs(t, m)
	assert.Equal(t, infoNothing, pressed.info.kind, "the log key opens nothing there")
	assert.Equal(t, infoNothing, pressTerminal(t, m).info.kind, "and neither does the terminal key")
	assert.Equal(t, infoNothing, pressOlderLog(t, m).info.kind, "nor the one that steps back a run")
	assert.Empty(t, tmux.respawnPanes, "and nothing is paged into a pane")
}

// TestEnteringAScreenWithoutTheKeysClosesTheInfoPane pins that a log opened on the agents list
// does not follow the reader onto a screen with no key left to close it.
func TestEnteringAScreenWithoutTheKeysClosesTheInfoPane(t *testing.T) {
	for _, next := range []screen{screenArchive, screenSearch, screenReview} {
		tmux := &recordingTmux{}
		m, _ := logsModel(t, tmux)
		require.True(t, m.claimInfo(infoAgentLog, ""))

		m.enterScreen(next)

		assert.Equal(t, infoNothing, m.info.kind)
	}
}

// TestAgentLogContentKeyFollowsTheAgent pins that the panel's own log restages when the cursor
// moves.
func TestAgentLogContentKeyFollowsTheAgent(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)

	first := m.agentLogContent().Key

	m.listing.agents[0].agent.ID = "a2"
	second := m.agentLogContent().Key

	assert.NotEqual(t, first, second)
}

// TestAgentLogRespawnsOnACursorMove pins the same thing through the panel, that moving between
// two agents with the log up pages the log of the agent the cursor landed on.
func TestAgentLogRespawnsOnACursorMove(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := logsModel(t, tmux)
	m.listing.agents = append(m.listing.agents, agentItem{agent: agentstore.Agent{
		ID:         "a2",
		Name:       "billing",
		Pane:       "%8",
		WorkflowID: "w1",
	}, alive: true})

	m.claimInfo(infoAgentLog, "")
	m.stageInfo()
	require.Equal(t, []string{"%2"}, tmux.respawnPanes)
	require.Contains(t, tmux.respawns[0], "WEAVE_AGENT_ID='a1'")

	moved, _ := agentsList().move(m, 1)
	agent, ok := moved.logTarget()
	require.True(t, ok)
	require.Equal(t, "a2", agent.ID, "the cursor has to have landed on the other agent for this to mean anything")

	moved.applyStageTick(stageTickMsg{move: moved.staging})

	require.Len(t, tmux.respawns, 2)
	assert.Equal(t, "%2", tmux.respawnPanes[1], "the log pane is the one paged again")
	assert.Contains(t, tmux.respawns[1], "WEAVE_AGENT_ID='a2'")
}
