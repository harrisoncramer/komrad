package ui

import (
	"errors"

	"komrad/internal/ui/keys"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// errNoSavedScreen is an agent whose pane was never saved.
var errNoSavedScreen = errors.New("no saved screen")

// lastScreen is the saved screen of the agent the column renders for.
type lastScreen struct {
	agentID  string
	answered bool
	screen   *agentv1.GetLastScreenResponse
	err      error
	fitted   derived[screenSize, string]
}

type screenSize struct {
	width  int
	height int
}

type lastScreenMsg struct {
	agentID string
	screen  *agentv1.GetLastScreenResponse
	err     error
}

// screenedAgentID is the agent whose saved screen the column renders.
func (m *model) screenedAgentID() (string, bool) {
	screen, ok := m.active().(savedScreenRenderer)
	if !ok {
		return "", false
	}

	return screen.screenedAgentID(m)
}

func (a *archivedAgents) screenedAgentID(m *model) (string, bool) {
	row, ok := a.hoveredRow(m)
	if !ok {
		return "", false
	}

	item, ok := row.paneItem()

	return item.Agent.ID, ok
}

func (a *agentsScreen) screenedAgentID(m *model) (string, bool) {
	item, ok := a.hoveredResting(m)

	return item.Agent.ID, ok
}

func (w *worktreesScreen) screenedAgentID(m *model) (string, bool) {
	row, ok := w.hovered(m)
	if !ok {
		return "", false
	}

	occupant, ok := row.item.FirstOccupant()
	if !ok {
		return "", false
	}

	if live, ok := m.listing.LiveAgent(occupant.ID); ok && live.Alive {
		return "", false
	}

	return occupant.ID, true
}

func (m *model) isScreenShown() bool {
	_, ok := m.screenedAgentID()

	return ok
}

func (m *model) readLastScreenCmd() tea.Cmd {
	agentID, ok := m.screenedAgentID()
	if !ok {
		return nil
	}

	m.lastScreen = lastScreen{agentID: agentID}
	clients := m.api

	return func() tea.Msg {
		screen, err := clients.getLastScreen(agentID)

		return lastScreenMsg{
			agentID: agentID,
			screen:  screen,
			err:     err,
		}
	}
}

func (m *model) readMovedLastScreenCmd() tea.Cmd {
	agentID, ok := m.screenedAgentID()
	if !ok {
		m.lastScreen = lastScreen{}

		return nil
	}

	if agentID == m.lastScreen.agentID {
		return nil
	}

	return m.readLastScreenCmd()
}

func (m *model) applyLastScreen(msg lastScreenMsg) {
	if msg.agentID != m.lastScreen.agentID {
		return
	}

	m.lastScreen = lastScreen{
		agentID:  msg.agentID,
		answered: true,
		screen:   msg.screen,
		err:      msg.err,
	}
}

func (m *model) screenNotice() string {
	screen, ok := m.active().(savedScreenRenderer)
	if !ok {
		return ""
	}

	return screen.screenNotice(m)
}

func (a *archivedAgents) screenNotice(m *model) string {
	restore := m.keys.ArchiveMenu.Restore
	if target, ok := a.menuTarget(m); ok {
		if _, err := target.opensAgent(m.listing); err == nil {
			restore = m.keys.ArchiveMenu.RestoreOpen
		}
	}

	return "archived" + separated + "press " + m.menuChordLabel(m.keys.Menu.Archive, restore) + " to restore"
}

func (a *agentsScreen) screenNotice(m *model) string {
	item, ok := a.hoveredResting(m)
	if ok && item.Agent.HeldAt != nil {
		return "paused"
	}

	return "dormant" + separated + "press " + keys.Label(fixedKeys().Open) + " to restore"
}

func (w *worktreesScreen) screenNotice(*model) string {
	return "not running"
}

// columnHeight is the rows that render the block beside the list.
func (m *model) columnHeight() int {
	split, ok := m.columnSplit()
	if !ok {
		return 0
	}

	return split.rows
}
