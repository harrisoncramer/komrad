package ui

import (
	"testing"

	"weave/config"
	"weave/internal/engine"
	"weave/internal/ui/layout"
	"weave/internal/ui/panel"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func terminalModel(t *testing.T, tmux *recordingTmux) (model, string) {
	t.Helper()

	dir := t.TempDir()
	m := infoModel(tmux)
	m.listing.agents[0].Agent.ID = "a1"
	m.listing.agents[0].Agent.Cwd = dir

	return m, dir
}

func infoRespawns(tmux *recordingTmux) []string {
	dirs := make([]string, 0, len(tmux.respawnPanes))
	for i, pane := range tmux.respawnPanes {
		if pane == "%2" {
			dirs = append(dirs, tmux.respawnDirs[i])
		}
	}

	return dirs
}

func pressTerminal(t *testing.T, m model) model {
	t.Helper()

	next, _ := m.Update(keyMsg(string('T')))
	panel, ok := next.(model)
	require.True(t, ok)

	return panel
}

// TestTerminalOpensInTheHoveredAgentDirectory pins that the key splits a shell under the stage,
// rooted at the agent's own directory, and hands it the keyboard.
func TestTerminalOpensInTheHoveredAgentDirectory(t *testing.T) {
	tmux := &recordingTmux{}
	m, dir := terminalModel(t, tmux)

	m.stageSelection()
	require.Equal(t, 1, tmux.swaps, "the live agent is staged to begin with")

	shown := pressTerminal(t, m)

	assert.Equal(t, infoTerminal, shown.info.kind)
	assert.Equal(t, dir, shown.info.subject, "the shell is pinned to the directory it opened in")
	assert.Equal(t, 1, tmux.swaps, "the agent's own pane is left where it is")
	require.Len(t, tmux.respawns, 1)
	assert.Equal(t, "%2", tmux.respawnPanes[0], "the shell goes in the pane below")
	assert.Equal(t, "/bin/zsh", tmux.respawns[0], "the shell is named so the pane cannot rerun what it held last")
	assert.Equal(t, dir, tmux.respawnDirs[0])
	assert.Equal(t, []string{"%2"}, tmux.selected, "the shell takes the keyboard")
}

// TestTerminalReplacesOpenLogs pins that the shell takes the pane the logs were in rather than
// opening a second one beside them.
func TestTerminalReplacesOpenLogs(t *testing.T) {
	tmux := &recordingTmux{}
	m, dir := terminalModel(t, tmux)
	m.settings.RunLogsDir = t.TempDir()

	logs := pressLogs(t, m)
	require.Equal(t, infoRunLog, logs.info.kind)
	require.Len(t, tmux.belowAnchors, 1)

	shown := pressTerminal(t, logs)

	assert.Equal(t, infoTerminal, shown.info.kind)
	assert.Len(t, tmux.belowAnchors, 1, "the pane the logs were in is reused")
	assert.Equal(t, []string{"%2", "%2"}, tmux.respawnPanes)
	assert.Equal(t, dir, tmux.respawnDirs[1])
}

// TestTerminalRefocusesTheShellAlreadyThere pins that pressing the key again hands the shell the
// keyboard rather than respawning it, which would throw away whatever the user was in the middle
// of.
func TestTerminalRefocusesTheShellAlreadyThere(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := terminalModel(t, tmux)

	again := pressTerminal(t, pressTerminal(t, m))

	assert.Equal(t, infoTerminal, again.info.kind)
	assert.Len(t, tmux.respawns, 1, "the shell is left running")
	assert.Equal(t, []string{"%2", "%2"}, tmux.selected)
}

// TestTerminalStaysPutAsTheCursorMoves pins that the cursor landing on another agent leaves the
// shell running where it was opened.
func TestTerminalStaysPutAsTheCursorMoves(t *testing.T) {
	tmux := &recordingTmux{}
	m, first := terminalModel(t, tmux)

	second := t.TempDir()
	m.listing.agents = append(m.listing.agents, panel.AgentRow{Agent: panel.Agent{
		ID:         "a2",
		Name:       "billing",
		Cwd:        second,
		WorkflowID: "w1",
	}})

	opened := pressTerminal(t, m)
	require.Equal(t, first, opened.info.subject)

	moved, _ := opened.Update(keyMsg("j"))
	panel, ok := moved.(model)
	require.True(t, ok)

	assert.Equal(t, first, panel.info.subject)
	assert.Equal(t, []string{first}, infoRespawns(tmux))
}

// TestTerminalMovesWhenAskedOnAnotherAgent pins that the key pressed over an agent the shell is
// not rooted in moves it there.
func TestTerminalMovesWhenAskedOnAnotherAgent(t *testing.T) {
	tmux := &recordingTmux{}
	m, first := terminalModel(t, tmux)

	second := t.TempDir()
	m.listing.agents = append(m.listing.agents, panel.AgentRow{Agent: panel.Agent{
		ID:         "a2",
		Name:       "billing",
		Cwd:        second,
		WorkflowID: "w1",
	}})

	opened := pressTerminal(t, m)
	require.Equal(t, "back to the terminal", opened.terminalDesc())

	moved, _ := opened.Update(keyMsg("j"))
	panel, ok := moved.(model)
	require.True(t, ok)
	require.Equal(t, "terminal here", panel.terminalDesc(), "the legend says the key moves the shell")

	again := pressTerminal(t, panel)

	assert.Equal(t, second, again.info.subject)
	assert.Equal(t, []string{first, second}, infoRespawns(tmux))
	assert.Equal(t, []string{"%2", "%2"}, tmux.selected, "the shell takes the keyboard where it lands")
}

// TestTerminalStaysPutOnASiblingSharingTheDirectory pins that the key pressed over another agent
// rooted in the same directory leaves the shell running.
func TestTerminalStaysPutOnASiblingSharingTheDirectory(t *testing.T) {
	tmux := &recordingTmux{}
	m, dir := terminalModel(t, tmux)

	m.listing.agents = append(m.listing.agents, panel.AgentRow{Agent: panel.Agent{
		ID:         "a2",
		Name:       "billing",
		Cwd:        dir,
		WorkflowID: "w1",
	}})

	opened := pressTerminal(t, m)

	moved, _ := opened.Update(keyMsg("j"))
	panel, ok := moved.(model)
	require.True(t, ok)
	require.Equal(t, "back to the terminal", panel.terminalDesc())

	again := pressTerminal(t, panel)

	assert.Equal(t, dir, again.info.subject)
	assert.Equal(t, []string{dir}, infoRespawns(tmux), "one directory is one shell")
	assert.Equal(t, []string{"%2", "%2"}, tmux.selected, "the shell takes the keyboard")
}

// TestTerminalStaysPutOverAWorkflowHeader pins that a row holding no directory of its own leaves
// the shell where it is.
func TestTerminalStaysPutOverAWorkflowHeader(t *testing.T) {
	tmux := &recordingTmux{}
	m, dir := terminalModel(t, tmux)

	opened := pressTerminal(t, m)

	moved, _ := opened.Update(keyMsg("k"))
	panel, ok := moved.(model)
	require.True(t, ok)

	assert.Equal(t, dir, panel.info.subject)
	assert.Equal(t, []string{dir}, infoRespawns(tmux))

	// The header holds no directory, but the shell under it is still what the key asks for.
	pressTerminal(t, panel)

	assert.Equal(t, []string{dir}, infoRespawns(tmux))
	assert.Equal(t, []string{"%2", "%2"}, tmux.selected)
}

// TestTerminalRefusesToMoveToAMissingWorktree pins that the key pressed over an agent whose
// directory has gone reports why, leaves the shell already up alone, and still hands it the
// keyboard.
func TestTerminalRefusesToMoveToAMissingWorktree(t *testing.T) {
	tmux := &recordingTmux{}
	m, dir := terminalModel(t, tmux)

	m.listing.agents = append(m.listing.agents, panel.AgentRow{Agent: panel.Agent{
		ID:         "a2",
		Name:       "billing",
		Cwd:        "/no/such/worktree",
		WorkflowID: "w1",
	}})

	opened := pressTerminal(t, m)

	moved, _ := opened.Update(keyMsg("j"))
	panel, ok := moved.(model)
	require.True(t, ok)

	refused := pressTerminal(t, panel)

	assert.Equal(t, dir, refused.info.subject)
	assert.Equal(t, []string{dir}, infoRespawns(tmux))
	require.NotEmpty(t, refused.messages.items)
	assert.Contains(t, getNewestMessage(refused).summary, "Could not open a terminal")
	assert.Equal(t, []string{"%2", "%2"}, tmux.selected)
}

// TestTerminalSurvivesTheRestaging pins that the stage being drawn again, which happens on every
// refresh and every cursor move, does not respawn the shell underneath it.
func TestTerminalSurvivesTheRestaging(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := terminalModel(t, tmux)

	shown := pressTerminal(t, m)
	require.Len(t, tmux.respawns, 1)

	shown.stageSelection()
	shown.stageSelection()

	assert.Len(t, tmux.respawns, 1)
}

// TestTerminalRefusesAMissingDirectory pins that an agent whose worktree has been removed
// reports why rather than dropping a shell somewhere else.
func TestTerminalRefusesAMissingDirectory(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := terminalModel(t, tmux)
	m.listing.agents[0].Agent.Cwd = "/no/such/worktree"

	shown := pressTerminal(t, m)

	assert.Equal(t, infoNothing, shown.info.kind)
	assert.Empty(t, tmux.respawns)
	require.NotEmpty(t, shown.messages.items)
	assert.Contains(t, getNewestMessage(shown).summary, "Could not open a terminal")
}

// TestTerminalDoesNothingWithoutAnAgent pins that the key is inert on a workflow header, which
// holds no directory of an agent's own.
func TestTerminalDoesNothingWithoutAnAgent(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := terminalModel(t, tmux)
	m.agents = agentsScreen{}

	shown := pressTerminal(t, m)

	assert.Equal(t, infoNothing, shown.info.kind)
	assert.Empty(t, tmux.respawns)
}

// TestTerminalOnARemoteEngineOpensTheShellOverTheTransport covers an agent whose directory is
// on another machine, where checking it here fails and the pane has to be rooted at home.
func TestTerminalOnARemoteEngineOpensTheShellOverTheTransport(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := terminalModel(t, tmux)
	m.frame = layout.New(tmux, config.DefaultLayout(), layout.WithEngine(engine.Remote{Destination: "ubuntu@engine"}))
	m.frame.Attach("%0")
	m.listing.agents[0].Agent.Cwd = "/var/lib/weave/worktrees/01a0c466"

	shown := pressTerminal(t, m)

	assert.Equal(t, infoTerminal, shown.info.kind)
	assert.Equal(t, "/var/lib/weave/worktrees/01a0c466", shown.info.subject)
	assert.Empty(t, shown.messages.items, "nothing is stat'd on this machine")
	require.NotEmpty(t, tmux.respawns)
	last := len(tmux.respawns) - 1
	assert.Equal(t, "%2", tmux.respawnPanes[last])
	assert.Equal(t, `ssh -t ubuntu@engine cd '/var/lib/weave/worktrees/01a0c466' && exec "${SHELL:-sh}"`, tmux.respawns[last])
	assert.Empty(t, tmux.respawnDirs[last], "the pane is not rooted at a directory this machine lacks")
	assert.Equal(t, []string{"%2"}, tmux.selected)
}

// TestTerminalOnARemoteEngineRefusesAnAgentWithNoDirectory covers a cwd of ”, which the local
// stat refuses and the remote side has to refuse itself rather than open a shell at home.
func TestTerminalOnARemoteEngineRefusesAnAgentWithNoDirectory(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := terminalModel(t, tmux)
	m.frame = layout.New(tmux, config.DefaultLayout(), layout.WithEngine(engine.Remote{Destination: "ubuntu@engine"}))
	m.frame.Attach("%0")
	m.listing.agents[0].Agent.Cwd = ""

	shown := pressTerminal(t, m)

	assert.Equal(t, infoNothing, shown.info.kind)
	assert.Empty(t, infoRespawns(tmux), "no shell is opened in the pane below")
	require.Len(t, shown.messages.items, 1)
	assert.Equal(t, "Could not open a terminal", shown.messages.items[0].summary)
}
