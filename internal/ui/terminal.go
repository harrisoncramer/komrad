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

	return layout.Content{
		Key: "terminal",
		Dir: dir,
		// A pane respawned with no command reruns whatever it held last.
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

	up := m.terminalDir != "" && m.frame.IsOpen(layout.RegionInfo)

	// The shell already rooted there takes the keyboard.
	if up && agent.Cwd == m.terminalDir {
		return focus()
	}

	// A directory on the engine cannot be checked from here.
	if err := m.checkTerminalDir(agent.Cwd); err != nil {
		m.report("Could not open a terminal", err)

		// The shell up still takes the keyboard.
		if up {
			return focus()
		}

		return m, nil
	}

	if !m.claimTerminal(agent.Cwd) {
		return m, nil
	}
	m.stageTerminal()

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

func (m *model) claimTerminal(dir string) bool {
	pane := m.frame.Open(layout.RegionInfo)
	if pane == "" {
		return false
	}

	m.tmux.SetInfoPane(pane)
	m.frame.Claimed(layout.RegionInfo)
	m.terminalDir = dir
	m.reconcileSolo()

	// The stage is uncovered by handing the window back, so whatever was parked in it while it
	// was covered has to be put right before it is on screen again.
	m.restageScreen()

	return true
}

func (m model) closeTerminal() model {
	m.frame.Close(layout.RegionInfo)
	m.forgetTerminal()
	m.restageScreen()

	return m
}

func (m *model) forgetTerminal() {
	m.tmux.SetInfoPane("")
	m.terminalDir = ""
	m.reconcileSolo()
}

func (m *model) stageTerminal() {
	if m.terminalDir == "" {
		return
	}

	if argv := m.frame.ShellArgv(m.terminalDir); len(argv) > 0 {
		m.frame.Show(layout.RegionInfo, remoteTerminalContent(argv))

		return
	}

	m.frame.Show(layout.RegionInfo, terminalContent(m.shell, m.terminalDir))
}

// reconcileTerminal catches the info pane going away on its own.
func (m *model) reconcileTerminal() {
	if m.terminalDir == "" || m.frame.IsOpen(layout.RegionInfo) {
		return
	}

	m.forgetTerminal()
}

// findTerminalPane is the shell the picker stops at, empty while none is open.
func (m model) findTerminalPane() string {
	if m.frame == nil || m.terminalDir == "" {
		return ""
	}

	return m.frame.Pane(layout.RegionInfo)
}

// closeTerminalLeaving closes the shell when the screen being entered cannot hold it.
func (m *model) closeTerminalLeaving(next screen) {
	if next.opensTerminal() || m.terminalDir == "" {
		return
	}

	*m = m.closeTerminal()
}
