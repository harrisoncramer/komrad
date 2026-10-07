package ui

import (
	"regexp"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/reader"
	"komrad/lib/xslice"
)

// saidRows is the turns an agent was found saying a says: qualifier's text in, wrapped to the
// column with the text marked where it appears.
func saidRows(text string, snippets []string, width int) []string {
	pattern := regexp.MustCompile("(?i)" + regexp.QuoteMeta(text))

	rows := []string{}
	for i, snippet := range snippets {
		if i > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, xslice.Map(paint.WrapRows(flatSnippet(snippet), width), func(row string) string {
			return paint.Panel.HighlightStyled(row, reader.Spans(pattern, row))
		})...)
	}

	return rows
}

// flatSnippet is a search hit's snippet as one run of text, without its match markers.
func flatSnippet(snippet string) string {
	unmarked := strings.NewReplacer(paint.MarkOpen, "", paint.MarkClose, "").Replace(snippet)

	return strings.Join(strings.Fields(unmarked), " ")
}

// saidColumnRows is the turns the agent was found saying a says: qualifier's text in, drawn for
// the column, and none when the filter found it saying nothing.
func (m *model) saidColumnRows(agentID string, width int) []string {
	snippets := m.filter.saidHits(agentID)
	if len(snippets) == 0 {
		return nil
	}

	return saidRows(m.filter.qualifiers.said.text, snippets, max(width, 1))
}
