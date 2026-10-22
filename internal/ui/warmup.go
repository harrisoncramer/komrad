package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// warmupPoll is how often a pane komrad is waiting on is asked whether its harness has started.
const warmupPoll = 100 * time.Millisecond

// warmupCap is how long a pane is waited on before it is taken as warm regardless, so a launch
// that never starts a harness does not hold a parked write forever.
const warmupCap = 4 * time.Second

type warmupTickMsg struct{}

func (m *model) warmupTick() tea.Cmd {
	return m.clock.After(warmupPoll, func(time.Time) tea.Msg { return warmupTickMsg{} })
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

// isPaneWarm reports whether a pane's harness has rendered itself, or the wait on it has run out.
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
	if m.isShowingHarness(pane, harnessID) {
		m.warm.markWarm(pane)

		return true
	}

	since, waiting := m.warm.since[pane]
	if !waiting {
		m.warm.since[pane] = m.clock.Now()

		return false
	}

	if m.clock.Now().Sub(since) < warmupCap {
		return false
	}

	m.warm.markWarm(pane)

	return true
}

// isShowingHarness reports whether a pane is showing the harness that was typed into it rather
// than the shell it was typed at.
func (m *model) isShowingHarness(pane, harnessID string) bool {
	if !m.tmux.IsPaneRunningCLI(pane) {
		return false
	}

	// A panel given no reader takes the foreground program for the answer.
	if m.harnessFrames == nil {
		return true
	}

	return m.harnessFrames.DoesFrameShowHarness(pane, harnessID)
}

func (w *warmupState) markWarm(pane string) {
	w.ready[pane] = true
	delete(w.since, pane)
}

// applyWarmupTick finishes the parked writes that have settled, forgetting the panes no write
// waits on any longer.
func (m *model) applyWarmupTick() tea.Cmd {
	m.warm.polling = false

	for pane := range m.warm.since {
		if !m.isPaneAwaited(pane) {
			delete(m.warm.since, pane)
		}
	}

	return m.finishParked()
}

// pollWarmup keeps a tick going while a pane is still being waited on or a write is parked.
func (m *model) pollWarmup() tea.Cmd {
	if m.warm.polling || (len(m.warm.since) == 0 && len(m.writes.parked) == 0) {
		return nil
	}

	m.warm.polling = true

	return m.warmupTick()
}
