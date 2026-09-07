package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// warmupPoll is how often a pane weave is waiting on is asked whether its harness has started.
const warmupPoll = 100 * time.Millisecond

// warmupCap is how long a pane is waited on before it is shown regardless, so a launch that
// never starts a harness leaves the stage holding a placeholder forever.
const warmupCap = 4 * time.Second

type warmupTickMsg struct{}

func warmupTick() tea.Cmd {
	return tea.Tick(warmupPoll, func(time.Time) tea.Msg { return warmupTickMsg{} })
}

// warmupState is what the panel knows about panes whose harness may not have started yet: the
// ones it has confirmed, the ones it is still waiting on, and the pane a create is holding the
// stage for while the cursor catches up to the row.
type warmupState struct {
	ready   map[string]bool
	since   map[string]time.Time
	polling bool
	pending string
	// pendingHarness is the harness of the pane a create is holding the stage for, since the row
	// it belongs to is not in a listing to be read off yet.
	pendingHarness string
	// closesForm says the create's own form is still up behind the wait, so that it is taken down
	// in the same change that reveals the pane rather than leaving the panel drawn over the whole
	// window until the harness turns up.
	closesForm bool
}

func newWarmupState() warmupState {
	return warmupState{ready: map[string]bool{}, since: map[string]time.Time{}}
}

// isPaneWarm reports whether a pane may be swapped into the stage, which is once its harness has
// drawn itself there.
func (m *model) isPaneWarm(pane, harnessID string) bool {
	if pane == "" {
		return false
	}
	if m.warm.ready == nil {
		m.warm = newWarmupState()
	}
	if m.warm.ready[pane] {
		return true
	}

	// The foreground program says the harness started, which it does most of a second before it
	// paints, so what settles the wait is a frame the harness reads as its own.
	if m.hasHarnessDrawn(pane, harnessID) {
		m.markWarm(pane)

		return true
	}

	since, waiting := m.warm.since[pane]
	if !waiting {
		m.warm.since[pane] = time.Now()
		m.sizeWarmingWindow(pane)

		return false
	}

	if time.Since(since) < warmupCap {
		return false
	}

	m.markWarm(pane)

	return true
}

// hasHarnessDrawn reports whether a pane is showing the harness that was typed into it rather
// than the shell it was typed at.
func (m *model) hasHarnessDrawn(pane, harnessID string) bool {
	if !m.tmux.IsPaneRunningCLI(pane) {
		return false
	}

	// A panel given no reader takes the foreground program for the answer, which is what the
	// frame would settle if one could be classified.
	if m.harnessFrames == nil {
		return true
	}

	return m.harnessFrames.DoesFrameShowHarness(pane, harnessID)
}

// isPaneStageable reports whether a pane may be shown as the cursor lands on it, which holds back
// only a launch still in flight: a harness that has started and has yet to draw.
func (m *model) isPaneStageable(pane, harnessID string) bool {
	if pane == "" {
		return false
	}

	// Explainer: a pane sitting at its shell has nothing coming that waiting would catch, since
	// the launch either has not been typed yet or the harness that was typed there has gone. It
	// is asked ahead of the wait so that no timer is started for it, and left unmarked, since it
	// is a pane this will be asked about again rather than one weave has seen a harness in.
	if !m.warm.ready[pane] && !m.tmux.IsPaneRunningCLI(pane) {
		return true
	}

	return m.isPaneWarm(pane, harnessID)
}

func (m *model) markWarm(pane string) {
	m.warm.ready[pane] = true
	delete(m.warm.since, pane)
}

// sizeWarmingWindow puts a pane's own window at the size of the slot it is about to be shown in,
// which the periodic sizing pass does not reach until the agent turns up in a listing.
func (m *model) sizeWarmingWindow(pane string) {
	// A guest is standing in the panel's own window, so resizing its window would resize the
	// terminal weave is drawn in and take the whole layout down with it.
	if pane == m.frame.GuestPane() {
		return
	}

	width, height := m.frame.StageCols(), m.height
	if width > 0 && height > 0 {
		m.tmux.ResizeWindow(pane, width, height)
	}
}

// holdForWarmup remembers the pane a create wants shown once its harness is up, since the cursor
// only reaches the new row on the next listing and the stage would otherwise be given to whatever
// the cursor is still standing on.
func (m *model) holdForWarmup(pane, harnessID string) {
	m.warm.pending = pane
	m.warm.pendingHarness = harnessID
}

// applyWarmupTick shows the pane a create is holding the stage for once it is warm, and otherwise
// stages the selection again so a cursor sitting on a pane that has just come up catches it.
func (m model) applyWarmupTick() (model, tea.Cmd) {
	m.warm.polling = false

	if pane := m.warm.pending; pane != "" {
		if !m.isPaneWarm(pane, m.warm.pendingHarness) {
			return m, nil
		}

		m.warm.pending, m.warm.pendingHarness = "", ""

		return m.revealCreated(pane)
	}

	selected, _ := m.selectedAgentPane()
	for pane := range m.warm.since {
		if pane != selected {
			delete(m.warm.since, pane)
		}
	}

	if selected != "" && !m.warm.ready[selected] {
		m.stageSelection()
	}

	return m, nil
}

// selectedAgentPane is the pane of the row the cursor is standing on, empty when that row has
// none to give the stage.
func (m model) selectedAgentPane() (pane, harnessID string) {
	if m.screen != screenAgents || m.hasNoAgentPane() {
		return "", ""
	}

	r, ok := rowAt(m.agentRows(), m.tree.cursor)
	if !ok {
		return "", ""
	}

	return r.item.agent.Pane, r.item.agent.Harness
}

// revealCreated gives a created agent its pane and takes down the form that made it, both inside
// one change, so the panel is never drawn across the whole window between the two.
func (m model) revealCreated(pane string) (model, tea.Cmd) {
	m.frame.Begin()

	var cmd tea.Cmd
	if m.warm.closesForm {
		m.warm.closesForm = false
		m.held = heldWrite{}

		closed, closing := m.closeForm()
		if next, ok := closed.(model); ok {
			m = next
		}

		cmd = tea.Batch(closing, m.refreshCmd())
	}

	m.frame.Solo(false)
	m.resize()
	m.frame.ShowGuest(pane)
	m.frame.Flush()

	return m, cmd
}

// pollWarmup keeps a tick going while any pane is still being waited on, and stops it once none
// is, so a settled panel asks tmux nothing.
func (m model) pollWarmup() (model, tea.Cmd) {
	if m.warm.polling || (len(m.warm.since) == 0 && m.warm.pending == "") {
		return m, nil
	}

	m.warm.polling = true

	return m, warmupTick()
}
