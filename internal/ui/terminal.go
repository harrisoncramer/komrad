package ui

import (
	"os"

	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
)

// fallbackShell is what the terminal runs when tmux answers for no default shell.
const fallbackShell = "sh"

// terminalContent is a shell in a directory, keyed by that directory rather than by the agent so
// that moving between agents a workflow roots in one place leaves the shell alone.
func terminalContent(shell, dir string) layout.Content {
	if shell == "" {
		shell = fallbackShell
	}

	// Naming the shell matters: a pane respawned with no command reruns whatever it held last.
	return layout.Content{
		Key:    "terminal:" + dir,
		Dir:    dir,
		Render: func(layout.Target) ([]string, bool) { return []string{shell}, true },
	}
}

// terminalDir is where the shell belongs: under the cursor while it is on an agent, and where it
// already is otherwise, so passing over a workflow header does not pull it out from under you.
func (m model) terminalDir() string {
	agent, ok := m.logTarget()
	if !ok {
		return m.info.action
	}

	// An agent whose worktree has been removed has nowhere to put a shell.
	if _, err := os.Stat(agent.Cwd); err != nil {
		return m.info.action
	}

	return agent.Cwd
}

// openTerminal puts a shell in the pane below the stage, rooted at the hovered agent's directory.
func (m model) openTerminal() (tea.Model, tea.Cmd) {
	if m.setupPending {
		return m, nil
	}

	agent, ok := m.logTarget()
	if !ok {
		return m, nil
	}

	if _, err := os.Stat(agent.Cwd); err != nil {
		m.report("Could not open a terminal", err)
		return m, nil
	}

	// A shell already up follows the cursor on its own, so the key is only asking for the keyboard.
	if m.info.kind != infoTerminal || !m.frame.IsOpen(layout.RegionInfo) {
		if !m.claimInfo(infoTerminal, agent.Cwd) {
			return m, nil
		}
	}

	m.stageInfo()
	m.tmux.SelectPane(m.frame.Pane(layout.RegionInfo))

	return m, nil
}

func (m model) terminalDesc() string {
	if m.info.kind == infoTerminal {
		return "back to the terminal"
	}

	return "terminal here"
}
