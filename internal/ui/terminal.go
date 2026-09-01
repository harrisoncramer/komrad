package ui

import (
	"os"

	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
)

// fallbackShell is what the terminal runs when tmux answers for no default shell.
const fallbackShell = "sh"

// terminalContent is a shell in a directory, keyed by that directory rather than by the agent so
// the shell the user is typing in survives the restaging every refresh does.
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

// openTerminal puts a shell in the pane below the stage, rooted at the hovered agent's directory.
func (m model) openTerminal() (tea.Model, tea.Cmd) {
	if m.setupPending {
		return m, nil
	}

	agent, ok := m.logTarget()
	if !ok {
		return m, nil
	}

	// An agent whose worktree has been removed has nowhere to put a shell.
	dir := agent.Cwd
	if _, err := os.Stat(dir); err != nil {
		m.report("Could not open a terminal", err)

		return m, nil
	}

	// The shell that agent asked for is already up, so it only wants the keyboard.
	if m.info.kind == infoTerminal && m.info.action == dir && m.frame.IsOpen(layout.RegionInfo) {
		m.tmux.SelectPane(m.frame.Pane(layout.RegionInfo))

		return m, nil
	}

	if !m.claimInfo(infoTerminal, dir) {
		return m, nil
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
