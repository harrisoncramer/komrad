package ui

import (
	"os"

	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
)

// fallbackShell is what the terminal runs when tmux answers for no default shell.
const fallbackShell = "sh"

// terminalContent is a shell rooted where it was opened, keyed to be left alone when the region
// is drawn again.
func terminalContent(shell, dir string) layout.Content {
	if shell == "" {
		shell = fallbackShell
	}

	// Naming the shell matters: a pane respawned with no command reruns whatever it held last.
	return layout.Content{
		Key:    "terminal",
		Dir:    dir,
		Render: func(layout.Target) ([]string, bool) { return []string{shell}, true },
	}
}

// openTerminal puts a shell in the pane below the stage, rooted at the hovered agent's directory,
// and moves the shell there when the key is pressed over an agent it is not already rooted in.
func (m model) openTerminal() (tea.Model, tea.Cmd) {
	focus := func() (tea.Model, tea.Cmd) {
		m.tmux.SelectPane(m.frame.Pane(layout.RegionInfo))

		return m, nil
	}

	up := m.info.kind == infoTerminal && m.frame.IsOpen(layout.RegionInfo)

	agent, ok := m.logTarget()
	if !ok {
		// A row holding no directory of its own can only ask for the shell already there.
		if up {
			return focus()
		}

		return m, nil
	}

	// The shell is where the key asks for, so it wants the keyboard rather than a fresh start.
	if up && agent.Cwd == m.info.subject {
		return focus()
	}

	if _, err := os.Stat(agent.Cwd); err != nil {
		m.report("Could not open a terminal", err)

		// The shell up is still worth the keyboard, and the message says why it did not move.
		if up {
			return focus()
		}

		return m, nil
	}

	if !m.claimInfo(infoTerminal, agent.Cwd) {
		return m, nil
	}
	m.stageInfo()

	return focus()
}

func (m model) terminalDesc() string {
	if m.info.kind != infoTerminal {
		return "terminal here"
	}

	// Pressing the key over an agent the shell is not rooted in moves it, so say so.
	if agent, ok := m.logTarget(); ok && agent.Cwd != m.info.subject {
		return "terminal here"
	}

	return "back to the terminal"
}
