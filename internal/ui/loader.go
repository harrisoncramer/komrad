package ui

import (
	"time"

	"komrad/internal/loader"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/submitting"

	tea "charm.land/bubbletea/v2"
)

type loaderTickMsg struct{}

func (m *model) loaderTick() tea.Cmd {
	return m.clock.After(loader.Interval, func(time.Time) tea.Msg { return loaderTickMsg{} })
}

type loadingState struct {
	style   loader.Style
	step    int
	ticking bool
}

func (l loadingState) frame() string {
	return l.style.Frame(l.step)
}

func (m *model) isLoading() bool {
	if len(m.writes.inflight) > 0 {
		return true
	}

	screen, ok := m.active().(loadingReporter)

	return ok && screen.IsLoading(m.inputs())
}

func (m *model) spinLoader() tea.Cmd {
	if m.loading.ticking || !m.isLoading() {
		return nil
	}

	m.loading.ticking = true

	return m.loaderTick()
}

func (m *model) advanceLoader() tea.Cmd {
	m.loading.ticking = false
	if !m.isLoading() {
		return nil
	}

	m.loading.step++
	m.loading.ticking = true

	return m.loaderTick()
}

// heldWrite is the write the form on screen is held for.
func (m *model) heldWrite() (submitting.Write, bool) {
	if !m.writes.held.active {
		return submitting.Write{}, false
	}

	write, ok := m.writes.inflight[m.writes.held.write]

	return write, ok
}

// looseWrites are the writes in flight that no form on screen is held for.
func (m *model) looseWrites() []submitting.Write {
	loose := make([]submitting.Write, 0, len(m.writes.inflight))
	for id, write := range m.writes.inflight {
		if m.writes.held.owns(id) {
			continue
		}

		loose = append(loose, write)
	}

	return loose
}

func (m *model) submittingParams(width int) submitting.Params {
	return submitting.Params{
		Palette: paint.Panel,
		Frame:   m.loading.frame(),
		Now:     m.clock.Now(),
		Width:   width,
	}
}
