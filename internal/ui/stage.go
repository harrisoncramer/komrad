package ui

import (
	"fmt"
	"time"

	"komrad/internal/ui/layout"
	"komrad/internal/ui/panel"
	"komrad/lib/xptr"

	tea "charm.land/bubbletea/v2"
)

// releasePanes swaps the given agents' panes out of the stage and marks them locally as no
// longer alive.
func (m *model) releasePanes(agents []panel.Agent) {
	for _, agent := range agents {
		m.frame.ReleaseGuest(agent.Pane)
		if held, ok := m.listing.world.Agents[agent.ID]; ok {
			held.Alive = xptr.Ptr(false)
		}
	}

	m.listing.assemble(time.Now())
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

	return startTimer(stageDelay, func(time.Time) tea.Msg { return stageTickMsg{move: move} })
}

// applyStageTick stages what the cursor came to rest on and asks for what the screen reads of it.
func (m *model) applyStageTick(msg stageTickMsg) tea.Cmd {
	if msg.move != m.staging {
		return nil
	}

	return tea.Batch(m.active().stageResting(m), m.wakeCwdSyncCmd())
}

func (w *worktreesScreen) stageResting(m *model) tea.Cmd {
	w.stage(m)

	return tea.Batch(m.inspectWorktreeCmd(), m.readLastScreenCmd())
}

func (d *documentsScreen) stageResting(m *model) tea.Cmd {
	d.stage(m)

	return m.readDocumentsCmd()
}

func (a *archiveScreen) stageResting(m *model) tea.Cmd {
	a.stage(m)

	return tea.Batch(m.readLastScreenCmd(), m.inspectAgentWorktreeCmd(time.Now()))
}

func (w *schedulesScreen) stageResting(m *model) tea.Cmd {
	m.stageSelection()

	return m.inspectAgentWorktreeCmd(time.Now())
}

func (l *logsScreen) stageResting(m *model) tea.Cmd {
	return l.switchSource(m)
}

func (a *agentsScreen) stageResting(m *model) tea.Cmd {
	m.stageSelection()

	return tea.Batch(m.readLastScreenCmd(), m.inspectAgentWorktreeCmd(time.Now()))
}

func (w *searchScreen) stageResting(m *model) tea.Cmd {
	m.stageSelection()

	return tea.Batch(w.landOnRow(m), m.inspectAgentWorktreeCmd(time.Now()))
}

func (r *reviewScreen) stageResting(m *model) tea.Cmd {
	if !r.hold.atEditor {
		m.stageSelection()
	}

	return tea.Batch(r.markThreadReadCmd(m), m.inspectAgentWorktreeCmd(time.Now()))
}

// paneStatusRows is the rows the blank status line tmux draws over every pane takes from its window.
const paneStatusRows = 1

// sizeAgentWindows keeps every agent's own window at the size of the pane it is shown in.
func (m *model) sizeAgentWindows() {
	for _, item := range m.listing.agents {
		m.sizeAgentWindow(item.Agent.Pane)
	}
}

// stageSelection puts the pane of whatever the agents cursor is standing on beside the list.
func (m *model) stageSelection() {
	if m.isScratchOpen() || m.agents.isEditorOpen() || m.isLandingHeld(time.Now()) {
		return
	}

	m.frame.Begin()
	defer m.afterStaging()

	m.reconcileSolo()

	staged, ok := m.agents.hoveredPane(m)

	switch {
	case ok && m.isPaneStageable(staged.Agent.Pane, staged.Agent.Harness):
		m.sizeAgentWindow(staged.Agent.Pane)
		m.frame.ShowGuest(layout.Guest{Agent: staged.Agent.ID, Pane: staged.Agent.Pane})
	case m.frame.IsSolo() && m.isGuestLive():
	default:
		m.frame.Show(layout.RegionStage, newBlankContent())
	}

	m.stageTerminal()
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
	// terminal komrad is drawn in.
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

	for _, item := range m.listing.agents {
		if item.Agent.Pane == guest {
			return item.Alive
		}
	}

	return false
}
