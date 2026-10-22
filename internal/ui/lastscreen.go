package ui

import (
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

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

	return screen.ScreenedAgentID(m.inputs())
}

func (a *archivedAgents) ScreenedAgentID(in screen.Inputs) (string, bool) {
	row, ok := a.HoveredRow(in)
	if !ok {
		return "", false
	}

	item, ok := row.paneItem()

	return item.Agent.ID, ok
}

func (a *agentsScreen) ScreenedAgentID(in screen.Inputs) (string, bool) {
	item, ok := a.Resting(in)

	return item.Agent.ID, ok
}

func (w *worktreesScreen) ScreenedAgentID(in screen.Inputs) (string, bool) {
	row, ok := w.hovered(in)
	if !ok {
		return "", false
	}

	occupant, ok := row.item.FirstOccupant()
	if !ok {
		return "", false
	}

	if live, ok := in.Listing.LiveAgent(occupant.ID); ok && live.Alive {
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
		screen, err := clients.GetLastScreen(agentID)

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

	return screen.ScreenNotice(m.inputs())
}

func (a *archivedAgents) ScreenNotice(in screen.Inputs) string {
	restore := a.deps.keys.ArchiveMenu.Restore
	if target, ok := a.menuTarget(in); ok {
		if _, err := target.opensAgent(in.Listing); err == nil {
			restore = a.deps.keys.ArchiveMenu.RestoreOpen
		}
	}

	return "archived" + separated + "press " + menuChordLabel(a.deps.keys, a.deps.keys.Menu.Archive, restore) + " to restore"
}

func (a *agentsScreen) ScreenNotice(in screen.Inputs) string {
	return "dormant" + separated + "press " + keys.Label(fixedKeys().Open) + " to restore"
}

func (w *worktreesScreen) ScreenNotice(screen.Inputs) string {
	return "not running"
}

// columnHeight is the rows that render the block beside the list.
func (m *model) columnHeight() int {
	split := m.inputs().Room.Column
	if !split.On {
		return 0
	}

	return split.Rows
}
