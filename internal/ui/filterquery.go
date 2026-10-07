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

// queryGrammarer is a screen whose filter reads qualifiers, on the lists that read any.
type queryGrammarer interface {
	queryGrammar() (queryGrammar, bool)
}

// saidState is the rows the server found saying the text of a says: qualifier.
type saidState struct {
	text    string
	hits    map[string][]saidHit
	problem string
	waiting bool
	queued  bool
}

// saidHit is one turn the server found saying the text of a says: qualifier.
type saidHit struct {
	snippet string
	offset  int64
	length  int64
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
	return len(f.saidHits(key)) > 0
}

// saidHits is the turns the server found the row saying the text in, and none while it has not answered.
func (f filterState) saidHits(key string) []saidHit {
	if f.qualifiers.said.waiting {
		return nil
	}

	return f.qualifiers.said.hits[key]
}

type saysSettledMsg struct {
	text string
}

type saysFoundMsg struct {
	text string
	hits map[string][]saidHit
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

	return startTimer(saysWait, func(time.Time) tea.Msg { return saysSettledMsg{text: text} })
}

func (m *model) applySaysSettled(msg saysSettledMsg) tea.Cmd {
	said := m.filter.qualifiers.said
	if msg.text != said.text || !said.waiting {
		return nil
	}

	client := m.api.reads().agents

	return func() tea.Msg {
		hits, err := call(agentsTimeout, func(ctx context.Context) (map[string][]saidHit, error) {
			return searchSaid(ctx, client, msg.text)
		})

		return saysFoundMsg{text: msg.text, hits: hits, err: err}
	}
}

// searchSaid asks the server which agents' transcripts hold the text, with the turns it was found in keyed by agent id.
func searchSaid(ctx context.Context, client agentReads, text string) (map[string][]saidHit, error) {
	resp, err := client.SearchTranscripts(ctx, connect.NewRequest(&agentv1.SearchTranscriptsRequest{
		Query:      phrase(text),
		Pagination: &paginationv1.Pagination{Limit: xptr.Ptr[int32](saysLimit)},
	}))
	if err != nil {
		return nil, err
	}

	hits := map[string][]saidHit{}
	for _, hit := range slices.Concat(resp.Msg.GetMatches(), resp.Msg.GetNear()) {
		hits[hit.GetAgentId()] = append(hits[hit.GetAgentId()], saidHit{
			snippet: hit.GetSnippet(),
			offset:  hit.GetContentOffset(),
			length:  hit.GetContentLength(),
		})
	}

	return hits, nil
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
		filter.qualifiers.said.hits = msg.hits
	}
	m.reconcileSolo()
	if filter.typing {
		return
	}

	hovered := m.hoveredFilterKey()
	if hovered != filter.appliedAt && (!filter.isNarrowing() || m.isFilterRowShown(hovered)) {
		return
	}

	m.applyFilter()
}
