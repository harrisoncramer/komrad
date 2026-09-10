package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// transcriptPreviewBefore is how many rows of lead-in the preview opens with, so the match the
// hit was found on is read in what was being said around it.
const transcriptPreviewBefore = 12

// searchPreview is the transcript standing beside the hits: what it was drawn from, the rows it
// drew to, and where in them the words the hit matched on landed.
type searchPreview struct {
	// conversation is the markdown the rows are drawn from, held rather than only the rows, since
	// the column is wrapped again whenever the window changes width.
	conversation string
	spans        []span
	doc          docWindow
}

// searchColumn is the transcript drawn beside the hits, which stands whether or not anything has
// been fetched for it.
func searchColumn() columnSpec {
	return columnSpec{floor: columnFloor}
}

// holdPreview takes the transcript a hit was fetched for and draws it at the width it will be
// read in, opening it on the match.
func (m *model) holdPreview(conversation string, spans []span) {
	if strings.TrimSpace(conversation) == "" {
		conversation, spans = noTranscriptMarkdown, nil
	}

	m.search.preview = searchPreview{conversation: conversation, spans: spans}
	m.drawPreview()
}

// drawPreview wraps the transcript into the column it is drawn in and puts the window back on
// the match, where a rewrap has moved it.
func (m *model) drawPreview() {
	preview := &m.search.preview
	if preview.conversation == "" {
		return
	}

	split, ok := m.screenColumnSplit()
	if !ok {
		preview.doc = docWindow{}

		return
	}

	text, line := renderTranscript(preview.conversation, preview.spans, split.column)
	preview.doc.rows = strings.Split(strings.TrimRight(text, "\n"), "\n")

	preview.doc.top = 0
	if line > 0 {
		preview.doc.top = max(line-1-transcriptPreviewBefore, 0)
	}

	preview.doc.top = preview.doc.clamp(preview.doc.top, split.rows)
}

// searchPreviewView is the screenful of the transcript beside the hits, drawn without the
// markers the archive's document carries.
func (m model) searchPreviewView(split columnSplit) string {
	doc := m.search.preview.doc
	if len(doc.rows) == 0 {
		doc = docWindow{rows: docRows(m.searchPreviewNotice(), split.column)}
	}

	return doc.view(split.rows)
}

// searchPreviewNotice is what stands in the column before a transcript does.
func (m model) searchPreviewNotice() string {
	switch {
	case strings.TrimSpace(m.search.query) == "":
		return searchWaitingMarkdown
	case len(m.searchHits()) == 0:
		return noSearchHitsMarkdown
	default:
		return searchLoadingMarkdown
	}
}

// scrollPreview moves the transcript under the hits.
func (m model) scrollPreview(delta int) (tea.Model, tea.Cmd) {
	split, ok := m.screenColumnSplit()
	if !ok {
		return m, nil
	}

	m.search.preview.doc.scroll(delta, split.rows)

	return m, nil
}
