package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"komrad/internal/errui"
	"komrad/internal/searchsyntax"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"
	paginationv1 "komrad/proto/gen/pagination/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
)

// saysWait is how long typing rests before a says: qualifier goes to the server.
const saysWait = 120 * time.Millisecond

// saysLimit is the most transcript turns a says: qualifier reads the agents from.
const saysLimit = 100

// queryGrammar is the qualifiers one list's filter reads and the values it takes for each.
type queryGrammar struct {
	syntax searchsyntax.Grammar
	check  func(searchsyntax.Clause, time.Time) error
}

func (g queryGrammar) parse(text string, now time.Time) (*searchsyntax.Query, error) {
	parsed, err := searchsyntax.Parse(text, g.syntax)
	if err != nil {
		return nil, err
	}

	for _, clause := range parsed.Clauses {
		if err := g.check(clause, now); err != nil {
			return nil, err
		}
	}

	return parsed, nil
}

// queryGrammarer is a screen whose filter reads qualifiers.
type queryGrammarer interface {
	queryGrammar() queryGrammar
}

// saidState is the rows the server found saying the text of a says: qualifier.
type saidState struct {
	text    string
	found   map[string]bool
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

	f.qualifiers.said = saidState{text: text, waiting: text != "", queued: text != ""}
}

// hasSaid reports whether the server found the row saying the text, and false while it has not answered.
func (f filterState) hasSaid(key string) bool {
	return !f.qualifiers.said.waiting && f.qualifiers.said.found[key]
}

type saysSettledMsg struct {
	text string
}

type saysFoundMsg struct {
	text  string
	found map[string]bool
	err   error
}

// saysCmd waits out the typing before a changed says: qualifier is sent.
func (m *model) saysCmd() tea.Cmd {
	said := &m.filter.qualifiers.said
	if !said.queued {
		return nil
	}
	said.queued = false
	text := said.text

	return startTimer(saysWait, func(time.Time) tea.Msg { return saysSettledMsg{text: text} })
}

func (m *model) applySaysSettled(msg saysSettledMsg) tea.Cmd {
	said := m.filter.qualifiers.said
	if msg.text != said.text || !said.waiting {
		return nil
	}

	client := m.api.reads().agents

	return func() tea.Msg {
		found, err := call(agentsTimeout, func(ctx context.Context) (map[string]bool, error) {
			return searchSaid(ctx, client, msg.text)
		})

		return saysFoundMsg{text: msg.text, found: found, err: err}
	}
}

// searchSaid asks the server which agents' transcripts hold the text, keyed by agent id.
func searchSaid(ctx context.Context, client agentReads, text string) (map[string]bool, error) {
	resp, err := client.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
		Query:      phrase(text),
		Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](saysLimit)},
	}))
	if err != nil {
		return nil, err
	}

	found := map[string]bool{}
	for _, hit := range slices.Concat(resp.Msg.GetMatches(), resp.Msg.GetNear()) {
		found[hit.GetAgentId()] = true
	}

	return found, nil
}

// phrase quotes text so a transcript search reads it as one phrase.
func phrase(text string) string {
	return `"` + strings.ReplaceAll(text, `"`, "") + `"`
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
		filter.qualifiers.said.found = msg.found
	}
	if filter.typing {
		return
	}

	hovered := m.hoveredFilterKey()
	if hovered != filter.appliedAt && (!filter.isNarrowing() || m.isFilterRowShown(hovered)) {
		return
	}

	m.applyFilter()
}
