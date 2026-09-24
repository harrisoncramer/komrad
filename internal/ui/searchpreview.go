package ui

import (
	"strings"

	"komrade/internal/ui/paint"

	tea "charm.land/bubbletea/v2"
)

// transcriptPreviewBefore is how many rows of lead-in the preview opens with, so the match the
// hit was found on is read in what was being said around it.
const transcriptPreviewBefore = 12

// previewWheelStep is how many rows one scroll key moves the transcript.
const previewWheelStep = 3

// searchPreview is the transcript standing beside the list: what it was drawn from, the rows it
// drew to, and where in them the words the hit matched on landed.
type searchPreview struct {
	agentID string
	// conversation is the markdown the rows are drawn from, held for the next wrap of the column.
	conversation string
	spans        []paint.Span
	open         int
	doc          docWindow
}

// searchColumn is the transcript drawn beside the list, which stands whether or not anything has
// been fetched for it.
func searchColumn() columnSpec {
	return columnSpec{floor: columnFloor}
}

type holdPreviewParams struct {
	agentID      string
	conversation string
	spans        []paint.Span
	open         int
}

// holdPreview takes the transcript an agent was fetched for and draws it at the width it will be
// read in, opening it on the match asked for.
func (w *searchScreen) holdPreview(s panelShared, params holdPreviewParams) {
	conversation, spans := params.conversation, params.spans
	if strings.TrimSpace(conversation) == "" {
		conversation, spans = noTranscriptMarkdown, nil
	}

	w.preview = searchPreview{
		agentID:      params.agentID,
		conversation: conversation,
		spans:        spans,
		open:         params.open,
	}
	w.drawPreview(s)
}

func (w *searchScreen) openPreviewOn(s panelShared, open int) {
	w.preview.open = open
	w.drawPreview(s)
}

// drawPreview wraps the transcript into the column it is drawn in and puts the window back on
// the match, where a rewrap has moved it.
func (w *searchScreen) drawPreview(s panelShared) {
	preview := &w.preview
	if preview.conversation == "" {
		return
	}

	split, ok := s.draw.screenColumnSplit()
	if !ok {
		preview.doc = docWindow{}

		return
	}

	text, line := paint.Panel.RenderTranscript(paint.RenderTranscriptParams{
		Conversation: preview.conversation,
		Spans:        preview.spans,
		Open:         preview.open,
		Width:        split.column,
	})
	preview.doc.rows = strings.Split(strings.TrimRight(text, "\n"), "\n")

	preview.doc.top = 0
	if line > 0 {
		preview.doc.top = max(line-1-transcriptPreviewBefore, 0)
	}

	preview.doc.top = preview.doc.clamp(preview.doc.top, split.rows)
}

// scrollPreview moves the transcript beside the list, stopping at either end.
func (w *searchScreen) scrollPreview(s panelShared, pressed string) tea.Cmd {
	split, ok := s.draw.screenColumnSplit()
	if !ok {
		return nil
	}

	delta := previewWheelStep
	if isBound(pressed, s.keys.Column.ScrollUp) {
		delta = -delta
	}

	doc := &w.preview.doc
	doc.top = doc.clamp(doc.top+delta, split.rows)

	return nil
}

// previewView is the screenful of the transcript beside the list, drawn without the markers the
// archive's document carries.
func (w *searchScreen) previewView(s panelShared, split columnSplit) string {
	doc := w.preview.doc
	if len(doc.rows) == 0 {
		doc = docWindow{rows: paint.Panel.DocRows(w.previewNotice(s), split.column)}
	}

	return doc.view(split.rows)
}

// previewNotice is what stands in the column before a transcript does.
func (w *searchScreen) previewNotice(s panelShared) string {
	switch {
	case strings.TrimSpace(w.query) == "":
		return searchWaitingMarkdown
	case len(w.rows(s)) == 0:
		return noSearchHitsMarkdown
	default:
		return searchLoadingMarkdown
	}
}
