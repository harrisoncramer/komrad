package ui

import (
	"testing"

	agentstore "weave/internal/store/agent"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func terminalModel(t *testing.T, tmux *recordingTmux) (model, string) {
	t.Helper()

	dir := t.TempDir()
	m := infoModel(tmux)
	m.listing.agents[0].agent.ID = "a1"
	m.listing.agents[0].agent.Cwd = dir

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

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'T'}})
	panel, ok := next.(model)
	require.True(t, ok)

	return panel
}

// TestTerminalOpensInTheHoveredAgentDirectory pins that the key splits a shell under the stage,
// rooted at the agent's own directory, and hands it the keyboard, since a shell nobody can type
// into is no use.
func TestTerminalOpensInTheHoveredAgentDirectory(t *testing.T) {
	tmux := &recordingTmux{}
	m, dir := terminalModel(t, tmux)

	m.stageSelection()
	require.Equal(t, 1, tmux.swaps, "the live agent is staged to begin with")

	shown := pressTerminal(t, m)

	assert.Equal(t, infoTerminal, shown.info.kind)
	assert.Equal(t, dir, shown.info.action, "the shell is pinned to the directory it opened in")
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
	m.actionLogsDir = t.TempDir()

	logs := pressLogs(t, m)
	require.Equal(t, infoActionLog, logs.info.kind)
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

// TestTerminalFollowsTheCursor pins that the shell moves to whichever agent the cursor lands on,
// the way the logs below the stage do, rather than waiting to be asked again.
func TestTerminalFollowsTheCursor(t *testing.T) {
	tmux := &recordingTmux{}
	m, first := terminalModel(t, tmux)

	second := t.TempDir()
	m.listing.agents = append(m.listing.agents, agentItem{agent: agentstore.Agent{
		ID:         "a2",
		Name:       "billing",
		Cwd:        second,
		WorkflowID: "w1",
	}})

	opened := pressTerminal(t, m)
	require.Equal(t, first, opened.info.action)

	moved, _ := opened.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	panel, ok := moved.(model)
	require.True(t, ok)

	assert.Equal(t, second, panel.info.action)
	assert.Equal(t, []string{first, second}, infoRespawns(tmux))
}

// TestTerminalStaysPutOverAWorkflowHeader pins that a row holding no directory of its own leaves
// the shell where it is, since the cursor passes over a header on the way to every agent.
func TestTerminalStaysPutOverAWorkflowHeader(t *testing.T) {
	tmux := &recordingTmux{}
	m, dir := terminalModel(t, tmux)

	opened := pressTerminal(t, m)

	moved, _ := opened.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("k")})
	panel, ok := moved.(model)
	require.True(t, ok)

	assert.Equal(t, dir, panel.info.action)
	assert.Equal(t, []string{dir}, infoRespawns(tmux))

	// The header holds no directory, but the shell under it is still what the key asks for.
	pressTerminal(t, panel)

	assert.Equal(t, []string{dir}, infoRespawns(tmux))
	assert.Equal(t, []string{"%2", "%2"}, tmux.selected)
}

// TestTerminalStaysPutOverAMissingWorktree pins that an agent whose directory has gone leaves the
// shell where it is rather than dropping it somewhere else.
func TestTerminalStaysPutOverAMissingWorktree(t *testing.T) {
	tmux := &recordingTmux{}
	m, dir := terminalModel(t, tmux)

	m.listing.agents = append(m.listing.agents, agentItem{agent: agentstore.Agent{
		ID:         "a2",
		Name:       "billing",
		Cwd:        "/no/such/worktree",
		WorkflowID: "w1",
	}})

	opened := pressTerminal(t, m)

	moved, _ := opened.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	panel, ok := moved.(model)
	require.True(t, ok)

	assert.Equal(t, dir, panel.info.action)
	assert.Equal(t, []string{dir}, infoRespawns(tmux))
}

// TestTerminalStaysPutAcrossAWorkflowsAgents pins that agents a workflow roots in one directory
// share one shell, since keying on the directory means there is nothing to move.
func TestTerminalStaysPutAcrossAWorkflowsAgents(t *testing.T) {
	tmux := &recordingTmux{}
	m, dir := terminalModel(t, tmux)

	m.listing.agents = append(m.listing.agents, agentItem{agent: agentstore.Agent{
		ID:         "a2",
		Name:       "billing",
		Cwd:        dir,
		WorkflowID: "w1",
	}})

	opened := pressTerminal(t, m)

	moved, _ := opened.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	panel, ok := moved.(model)
	require.True(t, ok)

	assert.Equal(t, dir, panel.info.action)
	assert.Equal(t, []string{dir}, infoRespawns(tmux), "one directory is one shell")
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
	m.listing.agents[0].agent.Cwd = "/no/such/worktree"

	shown := pressTerminal(t, m)

	assert.Equal(t, infoNothing, shown.info.kind)
	assert.Empty(t, tmux.respawns)
	require.NotNil(t, shown.banner)
	assert.Contains(t, shown.banner.Summary, "Could not open a terminal")
}

// TestTerminalDoesNothingWithoutAnAgent pins that the key is inert on a workflow header, which
// holds no directory of an agent's own.
func TestTerminalDoesNothingWithoutAnAgent(t *testing.T) {
	tmux := &recordingTmux{}
	m, _ := terminalModel(t, tmux)
	m.tree = treeState{}

	shown := pressTerminal(t, m)

	assert.Equal(t, infoNothing, shown.info.kind)
	assert.Empty(t, tmux.respawns)
}
