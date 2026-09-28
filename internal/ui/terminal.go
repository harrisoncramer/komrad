package ui

import (
	"errors"
	"os"

	"komrad/internal/ui/layout"
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// fallbackShell is what the terminal runs when tmux answers for no default shell.
const fallbackShell = "sh"

var (
	errNoAgentDir    = errors.New("the agent has no directory")
	errNoEngineShell = errors.New("the engine is not reachable")
)

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

// remoteTerminalContent is a shell on the engine.
func remoteTerminalContent(argv []string) layout.Content {
	return layout.Content{
		Key:    "terminal",
		Render: func(layout.Target) ([]string, bool) { return argv, true },
	}
}

// openTerminal puts a shell in the pane below the stage, rooted at the agent's directory, and
// hands the keyboard to the shell already there when it is rooted in the same place.
func (m model) openTerminal(agent panel.Agent) (tea.Model, tea.Cmd) {
	focus := func() (tea.Model, tea.Cmd) {
		m.tmux.SelectPane(m.frame.Pane(layout.RegionInfo))

		return m, nil
	}

	up := m.info.kind == infoTerminal && m.frame.IsOpen(layout.RegionInfo)

	// The shell is where the key asks for, so it wants the keyboard rather than a fresh start.
	if up && agent.Cwd == m.info.subject {
		return focus()
	}

	// A directory on the engine cannot be checked from here.
	if err := m.checkTerminalDir(agent.Cwd); err != nil {
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

func (m model) checkTerminalDir(dir string) error {
	if dir == "" {
		return errNoAgentDir
	}

	if m.frame.IsRemote() {
		if len(m.frame.ShellArgv(dir)) == 0 {
			return errNoEngineShell
		}

		return nil
	}

	_, err := os.Stat(dir)

	return err
}
