package ui

import (
	"context"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/searchsyntax"
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

// saidState is the rows the server found saying the text of a says: qualifier.
type saidState struct {
	text    string
	hits    map[string][]list.SaidHit
	problem string
	waiting bool
	queued  bool
}

// askSaid notes a says: qualifier whose text changed, for the model to send to the server.
func (f *filterState) askSaid() {
	text := ""
	for _, clause := range f.qualifiers.clauses {
		if clause.Key == searchsyntax.KeySays {
			text = clause.Value
		}
	}
	if text == f.qualifiers.said.text {
		if f.qualifiers.said.problem != "" {
			f.fail(f.qualifiers.said.problem)
		}

		return
	}

	f.qualifiers.said = saidState{
		text:    text,
		waiting: text != "",
		queued:  text != "",
	}
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
	said := &m.filter.qualifiers.said
	if !said.queued {
		return nil
	}
	said.queued = false
	text := said.text

	return m.clock.After(saysWait, func(time.Time) tea.Msg { return saysSettledMsg{text: text} })
}

func (m *model) applySaysSettled(msg saysSettledMsg) tea.Cmd {
	said := m.filter.qualifiers.said
	grammar := m.filter.qualifiers.grammar
	if msg.text != said.text || !said.waiting || grammar == nil {
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
	filter := &m.filter
	if msg.text != filter.qualifiers.said.text {
		return
	}

	filter.qualifiers.said.waiting = false
	if msg.err != nil {
		filter.qualifiers.said.problem = errui.GetReason(msg.err)
		filter.fail(filter.qualifiers.said.problem)
	} else {
		filter.qualifiers.said.hits = msg.hits
	}
	m.reconcileSolo()
	if filter.typing {
		return
	}

	hovered := m.hoveredFilterKey()
	if hovered != filter.appliedAt && (!filter.pattern().IsNarrowing() || m.isFilterRowShown(hovered)) {
		return
	}

	m.applyFilter()
}
