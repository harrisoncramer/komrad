package ui

import (
	"komrad/internal/assemble"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// worldReactor acts on what a batch from the watch changed in the world.
type worldReactor interface {
	react(m *model, changed assemble.Changed) tea.Cmd
}

func newWorldReactors() []worldReactor {
	return []worldReactor{
		reviewWorldReactor{},
		newOfferNoticeReactor(),
		strandedHoldReactor{},
	}
}

// react hands what a batch changed in the world to every reactor.
func (m *model) react(changed assemble.Changed) tea.Cmd {
	return tea.Batch(xslice.Map(m.reactors, func(reactor worldReactor) tea.Cmd {
		return reactor.react(m, changed)
	})...)
}
