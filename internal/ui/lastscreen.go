package ui

import (
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
	fitted   screen.Derived[screenSize, string]
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

// columnHeight is the rows that render the block beside the list.
func (m *model) columnHeight() int {
	split := m.inputs().Room.Column
	if !split.On {
		return 0
	}

	return split.Rows
}
