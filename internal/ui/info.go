package ui

import (
	"weave/config"
	"weave/internal/ui/layout"
)

type infoKind int

const (
	infoNothing infoKind = iota

	infoSetup

	infoSetupLog
	infoTeardownLog
	infoAgentLog

	infoEditFile
)

type infoState struct {
	kind infoKind

	action string
}

func (m *model) claimInfo(kind infoKind, action string) bool {
	pane := m.frame.Open(layout.RegionInfo)
	if pane == "" {
		return false
	}

	m.tmux.SetInfoPane(pane)
	m.frame.Claimed(layout.RegionInfo)
	m.info.kind = kind
	m.info.action = action

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
	m.info.action = ""
}

func (m *model) stageInfo() {
	switch m.info.kind {
	case infoSetupLog:
		m.frame.Show(layout.RegionInfo, m.actionLogContent(config.ActionLogSetup))
	case infoTeardownLog:
		m.frame.Show(layout.RegionInfo, m.actionLogContent(config.ActionLogTeardown))
	case infoAgentLog:
		m.frame.Show(layout.RegionInfo, m.agentLogContent())
	case infoNothing, infoSetup, infoEditFile:
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
