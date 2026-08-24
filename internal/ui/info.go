package ui

import "weave/internal/ui/layout"

type infoKind int

const (
	infoNothing infoKind = iota
	infoLogs

	infoEdit
	infoSetup
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
	case infoLogs:
		m.frame.Show(layout.RegionInfo, newLogContent(m.logPath, logArgv(m.logViewer, m.logPath)))
	case infoNothing, infoEdit, infoSetup:
	}
}

func (m *model) reconcileInfo() {
	if m.info.kind == infoNothing || m.frame.IsOpen(layout.RegionInfo) {
		return
	}

	m.forgetInfo()
}
