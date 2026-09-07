package ui

import (
	"weave/internal/ui/layout"
)

type infoKind int

const (
	infoNothing infoKind = iota
	infoRunLog
	infoAgentLog
	infoTerminal
	infoEditFile
)

type infoState struct {
	kind    infoKind
	subject string
	// run is how far back through an agent's runs the run-log pane has stepped, zero being
	// the most recent, so the pane follows the newest run rather than a path that went stale.
	run int
}

func (m *model) claimInfo(kind infoKind, subject string) bool {
	pane := m.frame.Open(layout.RegionInfo)
	if pane == "" {
		return false
	}

	m.tmux.SetInfoPane(pane)
	m.frame.Claimed(layout.RegionInfo)
	m.info.kind = kind
	m.info.subject = subject
	m.info.run = 0
	m.reconcileSolo()

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
	m.info.kind = infoNothing
	m.info.subject = ""
	m.info.run = 0
	m.reconcileSolo()
}

func (m *model) stageInfo() {
	switch m.info.kind {
	case infoRunLog:
		m.frame.Show(layout.RegionInfo, m.runLogContent())
	case infoAgentLog:
		m.frame.Show(layout.RegionInfo, m.agentLogContent())
	case infoTerminal:
		m.frame.Show(layout.RegionInfo, terminalContent(m.shell, m.info.subject))
	case infoNothing, infoEditFile:
	}
}

// reconcileInfo catches the pane going away on its own, which is what an editor quitting does,
// and reports whether what went was the editor: the file may have been changed or committed
// while it was up, so the diff behind it is stale.
func (m *model) reconcileInfo() bool {
	if m.info.kind == infoNothing || m.frame.IsOpen(layout.RegionInfo) {
		return false
	}

	edited := m.info.kind == infoEditFile
	m.forgetInfo()

	return edited
}
