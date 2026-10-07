package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// warmupPoll is how often a pane komrad is waiting on is asked whether its harness has started.
const warmupPoll = 100 * time.Millisecond

// warmupCap is how long a pane is waited on before it is shown regardless, so a launch that
// never starts a harness does not hold the stage forever.
const warmupCap = 4 * time.Second

type warmupTickMsg struct{}

func warmupTick() tea.Cmd {
	return startTimer(warmupPoll, func(time.Time) tea.Msg { return warmupTickMsg{} })
}

// warmupState is what the panel knows about panes whose harness may not have started yet: the
// ones it has confirmed and the ones it is still waiting on.
type warmupState struct {
	ready   map[string]bool
	since   map[string]time.Time
	polling bool
}

func newWarmupState() warmupState {
	return warmupState{ready: map[string]bool{}, since: map[string]time.Time{}}
}

// isPaneWarm reports whether a pane may be swapped into the stage, which holds once its harness
// has rendered itself there.
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
	if m.hasHarnessRendered(pane, harnessID) {
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

// hasHarnessRendered reports whether a pane is showing the harness that was typed into it rather
// than the shell it was typed at.
func (m *model) hasHarnessRendered(pane, harnessID string) bool {
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
// a launch or a kill still in flight.
func (m *model) isPaneStageable(pane, harnessID string) bool {
	if pane == "" {
		return false
	}

	if _, inFlight := m.unstaging[pane]; inFlight {
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

// applyWarmupTick stages the selection again so a cursor sitting on a pane that has just come up
// catches it, and finishes the parked writes that have settled.
func (m *model) applyWarmupTick() tea.Cmd {
	m.warm.polling = false

	selected, _ := m.selectedAgentPane()
	for pane := range m.warm.since {
		if pane != selected && !m.isPaneAwaited(pane) {
			delete(m.warm.since, pane)
		}
	}

	if selected != "" && !m.warm.ready[selected] {
		m.stageSelection()
	}

	return m.finishParked()
}

// selectedAgentPane is the pane of the row the cursor is standing on, empty when that row has
// none to give the stage.
func (m *model) selectedAgentPane() (pane, harnessID string) {
	if m.screen != screenAgents {
		return "", ""
	}

	item, ok := m.agents.hoveredPane(m)
	if !ok {
		return "", ""
	}

	return item.Agent.Pane, item.Agent.Harness
}

// pollWarmup keeps a tick going while a pane is still being waited on or a write is parked.
func (m *model) pollWarmup() tea.Cmd {
	if m.warm.polling || (len(m.warm.since) == 0 && len(m.writes.parked) == 0) {
		return nil
	}

	m.warm.polling = true

	return warmupTick()
}
