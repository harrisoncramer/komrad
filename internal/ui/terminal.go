package ui

import (
	"os"

	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
)

// terminalContent is a plain shell in a directory, keyed by that directory rather than by the
// agent so the shell the user is typing in survives the restaging every refresh does.
func terminalContent(dir string) layout.Content {
	return layout.Content{
		Key:    "terminal:" + dir,
		Dir:    dir,
		Render: func(layout.Target) ([]string, bool) { return nil, true },
	}
}

// openTerminal puts a shell in the pane below the stage rooted at the hovered agent's own
// directory and hands it the keyboard, moving a shell already there rather than opening a
// second one, and only refocusing it when it is already rooted where that agent asked.
func (m model) openTerminal() (tea.Model, tea.Cmd) {
	if m.setupPending {
		return m, nil
	}

	agent, ok := m.logTarget()
	if !ok {
		return m, nil
	}

	dir := agent.Cwd
	if _, err := os.Stat(dir); err != nil {
		m.report("Could not open a terminal", err)

		return m, nil
	}

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
