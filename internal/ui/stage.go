package ui

import (
	"fmt"
	"time"

	agentstore "weave/internal/store/agent"
	"weave/internal/ui/layout"

	tea "github.com/charmbracelet/bubbletea"
)

// releasePanes swaps the given agents' panes out of the stage and marks them locally as no
// longer alive.
func (m *model) releasePanes(agents []agentstore.Agent) {
	released := make(map[string]bool, len(agents))
	for _, agent := range agents {
		m.frame.ReleaseGuest(agent.Pane)
		released[agent.ID] = true
	}

	for i, item := range m.listing.agents {
		if released[item.agent.ID] {
			m.listing.agents[i].alive = false
		}
	}
}

// stageDelay is how long a cursor has to rest before the panes are moved to what it is standing
// on.
const stageDelay = 90 * time.Millisecond

// stageTickMsg is the rest a cursor move waited for, carrying the move it was owed to.
type stageTickMsg struct {
	move int
}

// deferStaging waits for the cursor to come to rest before the panes are moved.
func (m model) deferStaging() (model, tea.Cmd) {
	m.staging++
	move := m.staging

	return m, tea.Tick(stageDelay, func(time.Time) tea.Msg { return stageTickMsg{move: move} })
}

// applyStageTick stages what the cursor came to rest on and asks for what the screen reads of it.
func (m *model) applyStageTick(msg stageTickMsg) tea.Cmd {
	if msg.move != m.staging {
		return nil
	}

	if m.screen == screenWorktrees {
		m.stageWorktrees()

		return m.inspectWorktreeCmd()
	}

	m.stageSelection()

	return tea.Batch(m.listPullRequestsCmd(), m.inspectAgentWorktreeCmd(time.Now()))
}

// sizeAgentWindows keeps every agent's own window at the size of the pane it is shown in.
func (m *model) sizeAgentWindows() {
	if m.frame.IsRemote() {
		return
	}

	width, height := m.frame.StageCols(), m.height
	if width <= 0 || height <= 0 {
		return
	}

	if m.sized == nil {
		m.sized = map[string]string{}
	}

	want := fmt.Sprintf("%dx%d", width, height)
	guest := m.frame.GuestPane()

	for _, item := range m.listing.agents {
		pane := item.agent.Pane

		// The guest is standing in the panel's own window, so resizing its window would resize the
		// terminal weave is drawn in.
		if pane == "" || pane == guest || m.sized[pane] == want {
			continue
		}

		m.tmux.ResizeWindow(pane, width, height)
		m.sized[pane] = want
	}
}

// stageSelection puts the pane of whatever the cursor is standing on beside the list.
func (m *model) stageSelection() {
	m.frame.Begin()
	defer m.afterStaging()

	rows := m.agentRows()
	m.placeColumn(screenAgents, rowKeyAt(rows, m.tree.cursor))
	m.reconcileSolo()

	r, ok := rowAt(rows, m.tree.cursor)
	staged, held := r.paneItem()

	switch {
	case ok && held && !m.hasNoAgentPane() && m.isPaneStageable(staged.agent.Pane, staged.agent.Harness):
		m.sizeGuestWindow(staged.agent.Pane)
		m.frame.ShowGuest(staged.agent.Pane)
	case m.frame.IsSolo() && m.isGuestLive():
	default:
		m.frame.Show(layout.RegionStage, newBlankContent())
	}

	m.stageInfo()
}

// afterStaging sends the pane moves staging collected.
func (m *model) afterStaging() {
	m.frame.Flush()
}

// sizeGuestWindow puts an agent's own window at the size of the stage before it is swapped in.
func (m *model) sizeGuestWindow(pane string) {
	// A remote agent's window is sized by the ssh client attached to it, which is the stage pane
	// itself, so there is nothing here to set and the pane id names a window on another server.
	if m.frame.IsRemote() {
		return
	}

	width, height := m.frame.StageCols(), m.height
	if pane == "" || pane == m.frame.GuestPane() || width <= 0 || height <= 0 {
		return
	}

	if m.sized == nil {
		m.sized = map[string]string{}
	}

	want := fmt.Sprintf("%dx%d", width, height)
	if m.sized[pane] == want {
		return
	}

	m.tmux.ResizeWindow(pane, width, height)
	m.sized[pane] = want
}

// isGuestLive reports whether the pane parked in the stage still belongs to an agent the last
// listing found running.
func (m model) isGuestLive() bool {
	guest := m.frame.GuestPane()
	if guest == "" {
		return true
	}

	for _, item := range m.listing.agents {
		if item.agent.Pane == guest {
			return item.alive
		}
	}

	return false
}
