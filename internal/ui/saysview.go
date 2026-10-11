package ui

import (
	"slices"
	"strings"

	"komrad/internal/errui"
	"komrad/internal/ui/paint"
)

// saidColumnRows is the conversation of an agent the server found saying the text of a says:
// qualifier, rendered for the column, and none when it was found saying nothing.
func (m *model) saidColumnRows(agentID string, width int) []string {
	hits := m.filter.saidHits(agentID)
	if len(hits) == 0 {
		return nil
	}

	if !m.said.isSaidRead(agentID, m.filter.qualifiers.said.text) || !m.said.answered {
		return paint.DocStyledRows("_reading the conversation…_", max(width, 1), columnStyle())
	}

	if m.said.rows != nil && m.said.width == width {
		return m.said.rows
	}

	rows, _ := saidRows(saidRowsParams{
		said:  m.said,
		hits:  hits,
		width: width,
	})

	return rows
}

type saidRowsParams struct {
	said  saidTranscript
	hits  []saidHit
	width int
}

// saidRows is a conversation rendered for the column with every word the server matched in it
// marked, and the line the first of them is on, or one line saying why there is none.
func saidRows(params saidRowsParams) ([]string, int) {
	width := max(params.width, 1)
	switch {
	case params.said.err != nil:
		return paint.DocStyledRows("_could not read the conversation: "+errui.GetReason(params.said.err)+"_", width, columnStyle()), 0
	case strings.TrimSpace(params.said.content) == "":
		return paint.DocStyledRows("_no transcript yet_", width, columnStyle()), 0
	}

	content := params.said.content
	spans := []paint.Span{}
	for _, hit := range params.hits {
		spans = append(spans, paint.MatchSpans(paint.MatchSpansParams{
			Content: content,
			Offset:  hit.offset,
			Length:  hit.length,
			Terms:   paint.MarkedTerms(hit.snippet),
		})...)
	}
	slices.SortFunc(spans, func(x, y paint.Span) int { return x.Start - y.Start })

	text, line := paint.Panel.RenderTranscript(paint.RenderTranscriptParams{
		Conversation: content,
		Spans:        spans,
		Width:        width,
	})

	return strings.Split(strings.TrimRight(text, "\n"), "\n"), line
}
