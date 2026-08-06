package ui

import (
	"weave/internal/tmux"
	"weave/internal/ui/layout"
)

// What the panel decides to put in the pane under the stage. The layout package owns the pane
// itself, and this is the policy above it.

// bottomKind is what the panel has put in the region under the stage. Only one thing is down
// there at a time, and the key that opened it takes it away again.
type bottomKind int

const (
	bottomNothing bottomKind = iota
	bottomRecord
	bottomLogs
	// bottomEdit is an action's own file open in the user's editor, a kind of its own because
	// the key that closes it is decided by which file is down there rather than by which
	// script put it there.
	bottomEdit
)

// bottomState is what the frame's bottom region is holding. The stage above follows the cursor
// and this does not, apart from the record, which is about whichever agent is hovered.
type bottomState struct {
	kind bottomKind
	// action names the action whose file is open in the editor down there, so the key that
	// opened it is the key that takes it away.
	action string
	// dismissed says the hovered agent's record has been closed by killing the pane it was in,
	// so it is not reopened on the next agent hovered.
	dismissed bool
}

// claimBottom opens the region for something that fills it out of process, which actions do
// after reading their target pane from tmux.
func (m *model) claimBottom(kind bottomKind, action string) bool {
	pane := m.frame.Open(layout.RegionBottom)
	if pane == "" {
		return false
	}

	m.tmux.SetPanelGlobal(tmux.SecondaryPaneOption, pane)
	m.frame.Claimed(layout.RegionBottom)
	m.bottom.kind = kind
	m.bottom.action = action

	return true
}

// closeBottom takes away whatever the region is holding. Restaging afterward is what lets a
// hovered agent's record open back up when a log or an edit closes on it, rather than leaving
// the region closed until the next unrelated redraw finds it that way.
//
// The pane is left standing when restaging is about to fill it again. Killing it and splitting
// a new one in its place reflows the window twice, which a CLI in the stage answers with a full
// redraw each time and is seen as the panes stuttering.
func (m model) closeBottom() model {
	if !m.doesRestageFillBottom() {
		m.frame.Close(layout.RegionBottom)
	}

	m.forgetBottom()
	m.restageScreen()

	return m
}

// doesRestageFillBottom reports whether restaging would put something back in the region, which
// is the agents list drawing a hovered agent's record there.
func (m model) doesRestageFillBottom() bool {
	if m.screen != screenAgents || m.bottom.dismissed {
		return false
	}

	r, ok := rowAt(m.agentRows(), m.tree.cursor)

	return ok && !r.header
}

// forgetBottom clears the pane option, since tmux can reuse pane ids.
func (m *model) forgetBottom() {
	m.tmux.SetPanelGlobal(tmux.SecondaryPaneOption, "")
	m.bottom.kind = bottomNothing
	m.bottom.action = ""
}

// syncRecordDefault keeps the record's default open state in step with the row under the cursor,
// opening on an agent row unless the user has dismissed it and closing on a workflow header.
func (m *model) syncRecordDefault(onAgent bool) {
	switch {
	case onAgent && !m.bottom.dismissed && m.bottom.kind == bottomNothing:
		m.bottom.kind = bottomRecord
	case !onAgent:
		m.closeRecord()
	}
}

// closeRecord takes the region away if a record is what it is holding, which is what keeps it
// from holding onto whatever the agents list last opened while a log or an edit is down there.
func (m *model) closeRecord() {
	if m.bottom.kind != bottomRecord {
		return
	}

	m.frame.Close(layout.RegionBottom)
	m.forgetBottom()
}

// stageBottom draws whatever the region is holding, leaving an edit alone since the process
// that claimed the pane is what fills it.
func (m *model) stageBottom() {
	switch m.bottom.kind {
	case bottomRecord:
		r, ok := rowAt(m.agentRows(), m.tree.cursor)
		if !ok || r.header {
			return
		}

		m.stageRecord(agentMarkdown(r.item.agent, r.item.alive, m.scheduleName(r.item.agent)), layout.RegionBottom)
	case bottomLogs:
		m.frame.Show(layout.RegionBottom, newLogContent(m.logPath, logArgv(m.logViewer, m.logPath)))
	case bottomNothing, bottomEdit:
	}
}

// reconcileBottom notices a pane under the stage that has exited on its own, tmux's own way of
// taking it away. A record closed this way is the user dismissing it, so it is not put back the
// next time an agent is hovered.
func (m *model) reconcileBottom() {
	if m.bottom.kind == bottomNothing || m.frame.IsOpen(layout.RegionBottom) {
		return
	}

	if m.bottom.kind == bottomRecord {
		m.bottom.dismissed = true
	}

	m.forgetBottom()
}
