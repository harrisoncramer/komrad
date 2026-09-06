package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// searchPreviewFloor is the narrowest the transcript is worth wrapping into, which is about as
// short as a line of prose stays readable at.
const searchPreviewFloor = 40

// transcriptPreviewBefore is how many rows of lead-in the preview opens with, so the match the
// hit was found on is read in what was being said around it rather than at the top of the column.
const transcriptPreviewBefore = 12

// searchScrollStep is how far a scroll key moves the preview, kept short of a screenful so the
// lines around what was being read stay in view.
const searchScrollStep = 15

// searchPreview is the transcript standing beside the hits: what it was drawn from, the rows it
// drew to, and where in them the words the hit matched on landed.
type searchPreview struct {
	// conversation is the markdown the rows are drawn from, held rather than only the rows since
	// the column is wrapped again whenever the window changes width.
	conversation string
	spans        []span
	rows         []string
	// top is the first row the column is showing.
	top int
}

// searchSplit is how the window is divided while the search holds it: the transcript on one side
// and the panel's own column on the other.
type searchSplit struct {
	preview int
	sidebar int
	rows    int
}

// searchSplitOf divides a window between the preview and the sidebar, widening the configured
// share to what a list of hits can be read in and refusing a window that cannot hold both, where
// the sidebar takes the whole of it and the transcript waits for the room.
func searchSplitOf(width, rows, sidebar int) (searchSplit, bool) {
	if rows <= 0 || width < nameWidth+searchPreviewFloor {
		return searchSplit{}, false
	}

	sidebar = min(max(sidebar, nameWidth), width-searchPreviewFloor)

	return searchSplit{preview: width - sidebar, sidebar: sidebar, rows: rows}, true
}

// searchColumns is the window the preview is drawn across, false while the screen is not up,
// nothing has been fetched for the cursor to stand on, the frame is sharing the window with a
// pane of somebody else's, or what it holds is too narrow to carry both halves.
func (m model) searchColumns() (searchSplit, bool) {
	if m.screen != screenSearch || m.search.preview.conversation == "" || !m.frame.IsSolo() {
		return searchSplit{}, false
	}

	return searchSplitOf(m.width, m.height-m.bannerLines(), m.frame.SidebarWidth(m.width))
}

// holdPreview takes the transcript a hit was fetched for and draws it at the width it will be
// read in, opening it on the match the way the pager it replaced was positioned on one.
func (m *model) holdPreview(conversation string, spans []span) {
	if strings.TrimSpace(conversation) == "" {
		conversation, spans = noTranscriptMarkdown, nil
	}

	m.search.preview = searchPreview{conversation: conversation, spans: spans}
	m.drawPreview()
}

// drawPreview wraps the transcript into the column it is drawn in and puts the window back on
// the match, which is where the reader was last told to look and where a rewrap has moved.
func (m *model) drawPreview() {
	preview := &m.search.preview
	if preview.conversation == "" {
		return
	}

	split, ok := m.searchColumns()
	if !ok {
		preview.rows, preview.top = nil, 0

		return
	}

	text, line := renderTranscript(preview.conversation, preview.spans, split.preview)
	preview.rows = strings.Split(strings.TrimRight(text, "\n"), "\n")

	preview.top = 0
	if line > 0 {
		preview.top = max(line-1-transcriptPreviewBefore, 0)
	}

	preview.top = m.clampPreviewTop(preview.top, split)
}

// clampPreviewTop keeps the window inside the transcript, since a match near the end would
// otherwise open on rows that are not there and draw an empty column.
func (m model) clampPreviewTop(top int, split searchSplit) int {
	return max(min(top, len(m.search.preview.rows)-split.rows), 0)
}

// searchPreviewView is the screenful of the transcript beside the hits, squared off at its share
// of the window so the list cannot slide under a paragraph that fell short of the column.
func (m model) searchPreviewView(split searchSplit) string {
	rows := m.search.preview.rows
	start := m.clampPreviewTop(m.search.preview.top, split)

	return fitColumn(strings.Join(rows[start:min(start+split.rows, len(rows))], "\n"), split.preview)
}

// scrollPreview moves the transcript under the hits, which is the whole of what the pager it
// replaced was doing once it had opened on the match.
func (m model) scrollPreview(delta int) (tea.Model, tea.Cmd) {
	split, ok := m.searchColumns()
	if !ok {
		return m, nil
	}

	m.search.preview.top = m.clampPreviewTop(m.search.preview.top+delta, split)

	return m, nil
}
