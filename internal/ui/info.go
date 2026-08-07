package ui

import "weave/internal/ui/layout"

// What the panel decides to put in the info pane. The layout package owns the pane
// itself, and this is the policy above it.

// infoKind is what the panel has put in the info pane. Only one thing is down
// there at a time, and the key that opened it takes it away again.
type infoKind int

const (
	infoNothing infoKind = iota
	infoLogs
	// infoEdit is an action's own file open in the user's editor, a kind of its own because
	// the key that closes it is decided by which file is down there rather than by which
	// script put it there.
	infoEdit
	infoSetup
)

// infoState is what the frame's info pane is holding. Nothing here follows the cursor: the
// pane stays put as the user moves between the lists above it.
type infoState struct {
	kind infoKind
	// action names the action whose file is open in the editor down there, so the key that
	// opened it is the key that takes it away.
	action string
}

// claimInfo opens the region for something that fills it out of process, which actions do
// after reading their target pane from tmux.
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

// closeInfo takes the pane away and restages, since the stage reflows into the room the
// pane leaves behind and whatever is paged in it is laid out for the old height.
func (m model) closeInfo() model {
	m.frame.Close(layout.RegionInfo)
	m.forgetInfo()
	m.restageScreen()

	return m
}

// forgetInfo clears the pane option, since tmux can reuse pane ids.
func (m *model) forgetInfo() {
	m.tmux.SetInfoPane("")
	m.info.kind = infoNothing
	m.info.action = ""
}

// stageInfo draws whatever the region is holding, leaving an edit alone since the process
// that claimed the pane is what fills it.
func (m *model) stageInfo() {
	switch m.info.kind {
	case infoLogs:
		m.frame.Show(layout.RegionInfo, newLogContent(m.logPath, logArgv(m.logViewer, m.logPath)))
	case infoNothing, infoEdit, infoSetup:
	}
}

// reconcileInfo notices a pane under the stage that has exited on its own, tmux's own way of
// taking it away.
func (m *model) reconcileInfo() {
	if m.info.kind == infoNothing || m.frame.IsOpen(layout.RegionInfo) {
		return
	}

	m.forgetInfo()
}
