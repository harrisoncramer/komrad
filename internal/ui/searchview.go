package ui

import (
	"slices"
	"strings"
	"unicode"

	"komrad/internal/searchsyntax"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/view"
	"komrad/lib/xslice"

	"charm.land/bubbles/v2/key"
)

// spans locates every matched word in the transcript, in the order they are read.
func (a searchAgent) spans(content string) []paint.Span {
	var spans []paint.Span
	for _, hit := range a.turns {
		spans = append(spans, hit.spans(content)...)
	}

	slices.SortFunc(spans, func(x, y paint.Span) int { return x.Start - y.Start })

	return spans
}

// spans locates this hit's own matched words in the turn the hit names.
func (h searchHit) spans(content string) []paint.Span {
	return paint.MatchSpans(content, h.offset, h.length, paint.MarkedTerms(h.snippet))
}

// agentRow reads one agent the search found into the row the agents list draws.
func (w *searchScreen) agentRow(m *model, agent searchAgent, archived map[string]panel.AgentRow) view.Row {
	row := view.Row{
		Key:     agent.id,
		Kind:    view.KindResult,
		Name:    agent.name,
		Matches: nameMatches(agent.name, w.query()),
		Process: view.ProcessNone,
	}

	if agent.archived {
		return archivedSearchRow(row, agent.id, archived)
	}

	item, ok := m.listing.liveAgent(agent.id)
	if !ok {
		return row
	}

	return agentFacts(row, item)
}

func searchHitRow(row searchRow) view.Row {
	text, matches := flattenSnippet(row.hit.snippet)

	return view.Row{
		Key:      row.key(),
		Kind:     view.KindHit,
		Lead:     row.hit.lead(),
		Name:     text,
		Matches:  matches,
		Process:  view.ProcessNone,
		Archived: row.agent.archived,
	}
}

func flattenSnippet(snippet string) (string, []view.Span) {
	var (
		text    strings.Builder
		matches []view.Span
		open    = -1
		space   = true
	)
	for _, r := range snippet {
		switch {
		case r == rune(paint.MarkOpen[0]):
			open = text.Len()
		case r == rune(paint.MarkClose[0]):
			if open >= 0 && text.Len() > open {
				matches = append(matches, view.Span{Start: open, End: text.Len()})
			}
			open = -1
		case unicode.IsSpace(r):
			if !space {
				text.WriteRune(' ')
			}
			space = true
		default:
			text.WriteRune(r)
			space = false
		}
	}

	return strings.TrimSpace(text.String()), matches
}

// archivedSearchRow is a result on an archived agent.
func archivedSearchRow(row view.Row, id string, archived map[string]panel.AgentRow) view.Row {
	row.Process = view.ProcessStopped

	if item, ok := archived[id]; ok {
		row = agentFacts(row, item)
	}

	row.Archived = true

	return row
}

// snapshot is the rows the search found, as the list draws them.
func (w *searchScreen) snapshot(m *model, rows []searchRow, pane listPane) view.Snapshot {
	archived := keyAgentsByID(m.listing.archivedAgents)
	caret := pane.caret
	if w.isBlockFocused(m) {
		caret = view.CaretDrawn
	}

	return view.Snapshot{
		Rows: xslice.Map(rows, func(row searchRow) view.Row {
			if row.hit != nil {
				return searchHitRow(row)
			}

			return w.agentRow(m, row.agent, archived)
		}),
		Columns:    view.NoColumns(),
		Caret:      caret,
		BlinkedOut: pane.blinkedOut,
		Focused:    rowKeyAt(rows, w.cursor),
		Pane:       pane.width,
	}
}

// view draws the agents the search found beside the transcript of the hovered one.
func (w *searchScreen) view(m *model) string {
	sidebar := w.sidebarView(m)

	split, ok := m.screenColumnSplit()
	if !ok {
		return sidebar
	}

	return m.joinColumns(split, sidebar, w.previewView(m, split))
}

// sidebarView is the panel's own column: the query and the agents it found.
func (w *searchScreen) sidebarView(m *model) string {
	palette := paint.Panel

	if w.typing {
		palette = m.focusPalette()
	}

	query := filterState{typing: w.typing, input: w.input}.line(palette, m.glyphs)
	header := m.viewsBar() + query + "\n\n"

	blank := strings.TrimSpace(w.query()) == ""
	var qualifiers string
	if blank || m.helpExpanded {
		qualifiers = qualifierLegend(m.paneWidth())
	}
	help := m.renderHelp(w.help(m), header, qualifiers)
	if !blank {
		help = qualifiers + help
	}
	below := help
	if qualifiers != "" {
		below = "\n" + help
	}

	if w.problem.reason != "" {
		tone := paint.ToneDanger
		if w.problem.refused {
			tone = paint.ToneWarning
		}
		header += palette.Style(tone).Width(m.paneWidth()).Render(w.problem.reason) + "\n\n"
	}

	rows := w.rows(m)
	switch {
	case blank:
		return header + qualifiers + help
	case len(rows) == 0 && w.problem.reason != "":
		return header + help
	case len(rows) == 0:
		return header + dimStyle().Render("no matches") + "\n" + below
	}

	window := view.Window(view.WindowParams{
		List:    prepareList(m.glyphs, w.snapshot(m, rows, m.listPane(m.paneWidth()))),
		Palette: palette,
		Width:   m.paneWidth(),
		Lines:   m.listLines(header, below),
	})

	return header + window + below
}

// qualifierLegend lays out the qualifiers a search takes the way the legend lays out keys.
func qualifierLegend(width int) string {
	rows := xslice.Map(searchsyntax.Transcripts.Help, func(qualifier searchsyntax.Hint) key.Binding {
		return key.NewBinding(key.WithKeys(qualifier.Syntax), key.WithHelp(qualifier.Syntax, qualifier.Meaning))
	})

	return fitLegend(helpView(0).FullHelpView([][]key.Binding{rows}), width) + "\n\n"
}
