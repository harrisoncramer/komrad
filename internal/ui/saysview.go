package ui

import (
	"strings"

	"komrad/internal/ui/paint"
)

// saidColumnRows is the turns the agent was found saying a says: qualifier's text in, drawn for
// the column, and none when the filter found it saying nothing.
func (m *model) saidColumnRows(agentID string, width int) []string {
	snippets := m.filter.saidHits(agentID)
	if len(snippets) == 0 {
		return nil
	}

	return saidRows(snippets, width)
}

// saidRows is the turns an agent was found saying a says: qualifier's text in, wrapped to the
// column with what the server matched marked.
func saidRows(snippets []string, width int) []string {
	rows := []string{}
	for i, snippet := range snippets {
		if i > 0 {
			rows = append(rows, "")
		}

		text, marks := unmarkSnippet(snippet)
		at := 0
		for _, row := range paint.WrapRows(text, width) {
			if skipped := strings.Index(text[at:], row); skipped > 0 {
				at += skipped
			}
			start := at
			at += len(row)
			rows = append(rows, paint.Panel.HighlightStyled(row, spansWithin(marks, start, at)))
		}
	}

	return rows
}

// unmarkSnippet is a search hit's snippet as one run of text, and the stretches of it the server
// marked as matching.
func unmarkSnippet(snippet string) (string, []paint.Span) {
	rest := strings.Join(strings.Fields(snippet), " ")

	var text strings.Builder
	marks := []paint.Span{}
	for {
		before, after, found := strings.Cut(rest, paint.MarkOpen)
		text.WriteString(strings.ReplaceAll(before, paint.MarkClose, ""))
		if !found {
			return text.String(), marks
		}

		matched, after, _ := strings.Cut(after, paint.MarkClose)
		matched = strings.ReplaceAll(matched, paint.MarkOpen, "")
		marks = append(marks, paint.Span{Start: text.Len(), End: text.Len() + len(matched)})
		text.WriteString(matched)
		rest = after
	}
}

// spansWithin is the part of each span falling between start and end, counted from start.
func spansWithin(spans []paint.Span, start, end int) []paint.Span {
	within := []paint.Span{}
	for _, span := range spans {
		if span.End > start && span.Start < end {
			within = append(within, paint.Span{Start: max(span.Start, start) - start, End: min(span.End, end) - start})
		}
	}

	return within
}
