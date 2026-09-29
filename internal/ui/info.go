package ui

import (
	"komrad/internal/ui/layout"
)

func (m *model) claimInfo(dir string) bool {
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

func (m model) closeInfo() model {
	m.frame.Close(layout.RegionInfo)
	m.forgetInfo()
	m.restageScreen()

	return m
}

func (m *model) forgetInfo() {
	m.tmux.SetInfoPane("")
	m.terminalDir = ""
	m.reconcileSolo()
}

func (m *model) stageInfo() {
	if m.terminalDir == "" {
		return
	}

	if argv := m.frame.ShellArgv(m.terminalDir); len(argv) > 0 {
		m.frame.Show(layout.RegionInfo, remoteTerminalContent(argv))

		return
	}

	m.frame.Show(layout.RegionInfo, terminalContent(m.shell, m.terminalDir))
}

// reconcileInfo catches the info pane going away on its own.
func (m *model) reconcileInfo() {
	if m.terminalDir == "" || m.frame.IsOpen(layout.RegionInfo) {
		return
	}

	m.forgetInfo()
}
