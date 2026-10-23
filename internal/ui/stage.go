package ui

import (
	"fmt"
	"time"

	"komrad/internal/ui/layout"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xptr"

	tea "charm.land/bubbletea/v2"
)

// unstageAhead takes the given agents' panes off the stage before a write that kills them.
func (m *model) unstageAhead(agents []panel.Agent) {
	if m.unstaging == nil {
		m.unstaging = map[string]struct{}{}
	}

	staged := m.frame.GuestPane()
	m.frame.Begin()
	for _, agent := range agents {
		m.frame.ReleaseGuest(agent.Pane)
		if agent.Pane != "" {
			m.unstaging[agent.Pane] = struct{}{}
		}
	}

	if staged != "" && m.frame.GuestPane() == "" {
		m.frame.ClearStage()
	}
	m.frame.Flush()
}

// settleUnstaged marks the agents a write killed as no longer alive and stages the screen again.
func (m *model) settleUnstaged(agents []panel.Agent, killed bool) {
	for _, agent := range agents {
		delete(m.unstaging, agent.Pane)
	}

	if killed {
		for _, agent := range agents {
			if held, ok := m.listing.World.Agents[agent.ID]; ok {
				held.Alive = xptr.Ptr(false)
			}
		}

		m.listing.Assemble(m.clock.Now())
	}

	if stager, ok := m.active().(rowStager); ok {
		m.apply(stager.Stage())
	}
}

// stageDelay is how long a cursor has to rest before the panes are moved to what it is standing
// on.
const stageDelay = 90 * time.Millisecond

// stageTickMsg is the rest a cursor move waited for, carrying the move it was owed to.
type stageTickMsg struct {
	move int
}

// waitToStage waits for the cursor to come to rest before the panes are moved.
func (m *model) waitToStage() tea.Cmd {
	m.staging++
	move := m.staging

	return m.clock.After(stageDelay, func(time.Time) tea.Msg { return stageTickMsg{move: move} })
}

// applyStageTick stages what the cursor came to rest on and asks for what the screen reads of it.
func (m *model) applyStageTick(msg stageTickMsg) tea.Cmd {
	if msg.move != m.staging {
		return nil
	}

	return tea.Batch(m.apply(m.active().StageResting(m.inputs())), m.apply(m.inspectTaskWorktree(m.clock.Now())), m.wakeCwdSyncCmd())
}

func (a *agentsScreen) StageResting(screen.Inputs) screen.Request {
	return screen.Batch{screen.Restage{}, screen.ReadLastScreen{}}
}

// paneStatusRows is the rows the blank status line tmux renders over every pane takes from its window.
const paneStatusRows = 1

// sizeAgentWindows keeps every agent's own window at the size of the pane it is shown in.
func (m *model) sizeAgentWindows() {
	for _, item := range m.listing.Agents {
		m.sizeAgentWindow(item.Agent.Pane)
	}
}

// stageSelection puts the pane of whatever the agents cursor is standing on beside the list.
func (m *model) stageSelection() {
	if m.agents.isEditorOpen() {
		return
	}

	m.frame.Begin()
	defer m.afterStaging()

	staged, ok := m.stagedAgent()
	if !ok && m.wantsSolo() {
		m.reclaimStageKeys()
	}

	m.reconcileSolo()

	switch {
	case ok && m.isPaneStageable(staged.Agent.Pane):
		m.sizeAgentWindow(staged.Agent.Pane)
		m.frame.ShowGuest(layout.Guest{Agent: staged.Agent.ID, Pane: staged.Agent.Pane})
	case m.frame.IsSolo() && m.isGuestLive():
	default:
		m.frame.ClearStage()
	}

	m.stageTerminal()
}

// stagedAgent is the agent whose pane the stage shows, the one the screen keeps beside its block
// and the hovered agent on a screen that keeps none.
func (m *model) stagedAgent() (panel.AgentRow, bool) {
	if beside := m.active().Column(m.inputs()).Beside; beside != nil {
		return beside()
	}

	return m.agents.hoveredPane(m.inputs())
}

// reclaimStageKeys puts the keys back on the panel when they sit on the pane in the stage.
func (m *model) reclaimStageKeys() {
	if !m.term.keysElsewhere {
		return
	}

	active := m.tmux.GetActivePane(m.settings.Pane)
	if active == "" || (active != m.frame.GuestPane() && active != m.frame.Pane(layout.RegionStage)) {
		return
	}

	m.tmux.SelectPane(m.settings.Pane)
	m.term.keysElsewhere = false
}

// isPaneStageable reports whether a pane may be shown as the cursor lands on it, holding back
// a kill still in flight.
func (m *model) isPaneStageable(pane string) bool {
	if pane == "" {
		return false
	}

	_, inFlight := m.unstaging[pane]

	return !inFlight
}

// afterStaging sends the pane moves staging collected.
func (m *model) afterStaging() {
	m.frame.Flush()
}

// sizeAgentWindow puts an agent's own window at the size of the stage.
func (m *model) sizeAgentWindow(pane string) {
	// A remote agent's window is sized by the terminal stream attached to it, which is the stage
	// pane itself, so there is nothing here to set and the pane id names a window on another server.
	if m.frame.IsRemote() {
		return
	}

	width, height := m.frame.StageCols(), m.term.height+paneStatusRows

	// The guest is standing in the panel's own window, so resizing its window would resize the
	// terminal komrad is rendered in.
	if pane == "" || pane == m.frame.GuestPane() || width <= 0 || m.term.height <= 0 {
		return
	}

	if m.term.sized == nil {
		m.term.sized = map[string]string{}
	}

	want := fmt.Sprintf("%dx%d", width, height)
	if m.term.sized[pane] == want {
		return
	}

	m.tmux.ResizeWindow(pane, width, height)
	m.term.sized[pane] = want
}

// isGuestLive reports whether the pane parked in the stage still belongs to an agent the last
// listing found running.
func (m *model) isGuestLive() bool {
	guest := m.frame.GuestPane()
	if guest == "" {
		return true
	}

	for _, item := range m.listing.Agents {
		if item.Agent.Pane == guest {
			return item.Alive
		}
	}

	return false
}
