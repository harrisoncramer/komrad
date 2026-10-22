package ui

import (
	"context"
	"strings"
	"time"

	"komrad/internal/ui/api"
	"komrad/internal/ui/list"

	tea "charm.land/bubbletea/v2"
)

// saysWait is how long typing rests before a says: qualifier goes to the server.
const saysWait = 120 * time.Millisecond

// saysLimit is the most hits a says: qualifier reads the rows from.
const saysLimit = 100

// phrase quotes text so a search reads it as one phrase.
func phrase(text string) string {
	return `"` + strings.ReplaceAll(text, `"`, "") + `"`
}

// queryGrammarer is a screen whose filter reads qualifiers, and says whether the list in view reads any.
type queryGrammarer interface {
	QueryGrammar() (list.Grammar, bool)
}

type saysSettledMsg struct {
	text string
}

type saysFoundMsg struct {
	text string
	hits map[string][]list.SaidHit
	err  error
}

// saysCmd waits out the typing before a changed says: qualifier is sent.
func (m *model) saysCmd() tea.Cmd {
	text, queued := m.filter.TakeQueuedSaid()
	if !queued {
		return nil
	}

	return m.clock.After(saysWait, func(time.Time) tea.Msg { return saysSettledMsg{text: text} })
}

func (m *model) applySaysSettled(msg saysSettledMsg) tea.Cmd {
	said := m.filter.Pattern().Said
	grammar, reads := m.filter.Grammar()
	if msg.text != said.Text || !said.Waiting || !reads {
		return nil
	}

	search := grammar.Search

	return func() tea.Msg {
		hits, err := api.Call(agentsTimeout, func(ctx context.Context) (map[string][]list.SaidHit, error) {
			return search(ctx, msg.text)
		})

		return saysFoundMsg{
			text: msg.text,
			hits: hits,
			err:  err,
		}
	}
}

func (m *model) applySaysFound(msg saysFoundMsg) {
	search := &m.filter
	if !search.FindSaid(msg.text, msg.hits, msg.err) {
		return
	}

	m.reconcileSolo()
	if search.Typing {
		return
	}

	hovered := m.hoveredFilterKey()
	if hovered != search.AppliedAt && (!search.Pattern().IsNarrowing() || m.isFilterRowShown(hovered)) {
		return
	}

	m.applyFilter()
}
