package ui

import (
	"time"

	"komrad/internal/ui/layout"

	tea "charm.land/bubbletea/v2"
)

// warmupPoll is how often a pane komrad is waiting on is asked whether its harness has started.
const warmupPoll = 100 * time.Millisecond

// warmupCap is how long a pane is waited on before it is shown regardless, so a launch that
// never starts a harness does not hold the stage forever.
const warmupCap = 4 * time.Second

type warmupTickMsg struct{}

func warmupTick() tea.Cmd {
	return tea.Tick(warmupPoll, func(time.Time) tea.Msg { return warmupTickMsg{} })
}

// warmupState is what the panel knows about panes whose harness may not have started yet: the
// ones it has confirmed, the ones it is still waiting on, and the pane a create is holding the
// stage for.
type warmupState struct {
	ready          map[string]bool
	since          map[string]time.Time
	polling        bool
	pending        string
	pendingAgent   string
	pendingHarness string
	// closesForm says the create's own form is still up behind the wait, so the reveal takes it
	// down in the same change.
	closesForm bool
	// pendingFocus says the reveal hands the pane the keyboard.
	pendingFocus bool
}

func newWarmupState() warmupState {
	return warmupState{ready: map[string]bool{}, since: map[string]time.Time{}}
}

// isPaneWarm reports whether a pane may be swapped into the stage, which holds once its harness
// has drawn itself there.
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

	// The foreground program starts most of a second before the harness paints, so what settles
	// the wait is a frame the harness reads as its own.
	if m.hasHarnessDrawn(pane, harnessID) {
		m.warm.markWarm(pane)

		return true
	}

	since, waiting := m.warm.since[pane]
	if !waiting {
		m.warm.since[pane] = time.Now()

		return false
	}

	if time.Since(since) < warmupCap {
		return false
	}

	m.warm.markWarm(pane)

	return true
}

// hasHarnessDrawn reports whether a pane is showing the harness that was typed into it rather
// than the shell it was typed at.
func (m *model) hasHarnessDrawn(pane, harnessID string) bool {
	if !m.tmux.IsPaneRunningCLI(pane) {
		return false
	}

	// A panel given no reader takes the foreground program for the answer.
	if m.harnessFrames == nil {
		return true
	}

	return m.harnessFrames.DoesFrameShowHarness(pane, harnessID)
}

// isPaneStageable reports whether a pane may be shown as the cursor lands on it, holding back
// only a launch still in flight.
func (m *model) isPaneStageable(pane, harnessID string) bool {
	if pane == "" {
		return false
	}

	// A pane sitting at its shell has nothing coming that waiting would catch, and komrad has seen
	// no harness in it to mark.
	if !m.warm.ready[pane] && !m.tmux.IsPaneRunningCLI(pane) {
		return true
	}

	return m.isPaneWarm(pane, harnessID)
}

func (w *warmupState) markWarm(pane string) {
	w.ready[pane] = true
	delete(w.since, pane)
}

// hold remembers the pane a write wants shown once its harness is up.
func (w *warmupState) hold(guest layout.Guest, harnessID string, focus bool) {
	w.pending = guest.Pane
	w.pendingAgent = guest.Agent
	w.pendingHarness = harnessID
	w.pendingFocus = focus
}

// applyWarmupTick shows the pane a write is holding the stage for once it is warm, and otherwise
// stages the selection again so a cursor sitting on a pane that has just come up catches it.
func (m model) applyWarmupTick() (model, tea.Cmd) {
	m.warm.polling = false

	if pane := m.warm.pending; pane != "" {
		if !m.isPaneWarm(pane, m.warm.pendingHarness) {
			return m, nil
		}

		guest := layout.Guest{Agent: m.warm.pendingAgent, Pane: pane}
		focus := m.warm.pendingFocus
		m.warm.pending, m.warm.pendingAgent, m.warm.pendingHarness, m.warm.pendingFocus = "", "", "", false

		return m.revealCreated(guest, focus)
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
	if m.screen != screenAgents {
		return "", ""
	}

	item, ok := m.agents.hoveredPane(m.shared())
	if !ok {
		return "", ""
	}

	return item.Agent.Pane, item.Agent.Harness
}

// revealCreated gives an agent its pane and takes down the form that made it in one
// change, so the panel is never drawn across the whole window between the two.
func (m model) revealCreated(guest layout.Guest, focus bool) (model, tea.Cmd) {
	m.frame.Begin()

	var cmd tea.Cmd
	if m.warm.closesForm {
		m.warm.closesForm = false
		m.held = heldWrite{}

		closed, closing := m.closeForm()
		if next, ok := closed.(model); ok {
			m = next
		}

		cmd = closing
	}

	m.frame.Solo(false)
	m.frame.ShowGuest(guest)
	m.frame.Flush()

	if focus && m.frame.GuestPane() == guest.Pane {
		m.tmux.SelectPane(guest.Pane)
	}

	return m, cmd
}

// poll keeps a tick going while a pane is still being waited on.
func (w *warmupState) poll() tea.Cmd {
	if w.polling || (len(w.since) == 0 && w.pending == "") {
		return nil
	}

	w.polling = true

	return warmupTick()
}
