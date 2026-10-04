package ui

import (
	"komrad/internal/ui/layout"
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// reconcileChat puts the implementer's chat under the review when the review wants it and the
// window can spare the rows, and takes it down otherwise.
func (m *model) reconcileChat() {
	if guest, rows, ok := m.findChat(); ok {
		m.frame.ShowTile(guest, rows)
	} else if m.frame.TileGuest() != "" {
		m.frame.ReleaseTile()
	}

	m.reconcileSolo()
}

// findChat is the implementer whose chat belongs under the review and the rows it gets, false
// when the review is not up, the chat is switched off, the agent is not an implementer, or the
// window is too short.
func (m *model) findChat() (layout.Guest, int, bool) {
	if m.screen != screenReview || m.chatOff {
		return layout.Guest{}, 0, false
	}

	row, ok := m.review.agentRow(m)
	if !ok || row.Agent.Role != panel.RoleImplementer || row.Agent.Pane == "" {
		return layout.Guest{}, 0, false
	}

	rows := m.frame.TileRows(m.frame.WindowRows(), minScreenRows)
	if rows <= 0 {
		return layout.Guest{}, 0, false
	}

	return layout.Guest{Agent: row.Agent.ID, Pane: row.Agent.Pane}, rows, true
}

// findChatPane finds the pane the leader stops at for the implementer's chat, empty while the
// review shows none.
func (m *model) findChatPane() string {
	if m.screen != screenReview || m.frame == nil {
		return ""
	}

	return m.frame.TilePane()
}

// leaveScreen sends the implementer's chat home with the review going down.
func (r *reviewScreen) leaveScreen(m *model) tea.Cmd {
	if m.frame.TileGuest() != "" {
		m.frame.ReleaseTile()
	}

	return nil
}
