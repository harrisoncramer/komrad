package ui

import (
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint"

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
	row          int
	match        int
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
func (w *searchScreen) holdPreview(m *model, params holdPreviewParams) {
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
	w.drawPreview(m)
}

func (w *searchScreen) openPreviewOn(m *model, open int) {
	w.preview.open = open
	w.drawPreview(m)
}

// drawPreview wraps the transcript into the column it is drawn in and puts the window back on
// the match, where a rewrap has moved it.
func (w *searchScreen) drawPreview(m *model) {
	preview := &w.preview
	if preview.conversation == "" {
		return
	}

	split, ok := m.screenColumnSplit()
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
	preview.match = line - 1
	if line > 0 {
		preview.doc.top = max(line-1-transcriptPreviewBefore, 0)
	}

	preview.doc.top = preview.doc.clamp(preview.doc.top, split.rows)
}

// scrollPreview moves the transcript beside the list, stopping at either end.
func (w *searchScreen) scrollPreview(m *model, pressed string) tea.Cmd {
	if isBound(pressed, m.keys.Column.ScrollUp) {
		return w.scrollPreviewBy(m, -previewWheelStep)
	}

	return w.scrollPreviewBy(m, previewWheelStep)
}

func (w *searchScreen) scrollPreviewBy(m *model, rows int) tea.Cmd {
	split, ok := m.screenColumnSplit()
	if !ok {
		return nil
	}

	doc := &w.preview.doc
	doc.top = doc.clamp(doc.top+rows, split.rows)

	return nil
}

func (w *searchScreen) doesScrollColumn(*model) bool {
	return true
}

func (w *searchScreen) scrollColumn(m *model, rows int) tea.Cmd {
	return w.scrollPreviewBy(m, rows)
}

func (w *searchScreen) scrollList(m *model, rows int) tea.Cmd {
	return w.list().move(m, rows)
}

// previewView is the screenful of the transcript beside the list, drawn without the markers the
// archive's document carries.
func (w *searchScreen) previewView(m *model, split columnSplit) string {
	doc := w.preview.doc
	if len(doc.rows) == 0 {
		doc = docWindow{rows: paint.Panel.DocRows(w.previewNotice(m), split.column)}
	}

	if w.isBlockFocused(m) {
		line := w.previewLine(split.rows)
		doc.rows = slices.Clone(doc.rows)
		doc.rows[line] = paint.Panel.PaintRow(doc.rows[line], split.column)
	}

	return doc.view(split.rows)
}

// previewNotice is what stands in the column before a transcript does.
func (w *searchScreen) previewNotice(m *model) string {
	switch {
	case strings.TrimSpace(w.query()) == "":
		return searchWaitingMarkdown
	case len(w.rows(m)) == 0:
		return noSearchHitsMarkdown
	default:
		return searchLoadingMarkdown
	}
}

func (w *searchScreen) isBlockDrawn(m *model) bool {
	if _, ok := m.screenColumnSplit(); !ok {
		return false
	}

	return w.preview.conversation != "" && len(w.preview.doc.rows) > 0
}

func (w *searchScreen) isBlockFocused(m *model) bool {
	return w.previewFocused && m.isListFocused() && !w.typing && w.isBlockDrawn(m)
}

func (w *searchScreen) focusBlock(m *model) {
	w.previewFocused = true
	w.preview.row = 0

	split, ok := m.screenColumnSplit()
	if !ok {
		return
	}

	if row := w.preview.match - w.preview.doc.top; w.preview.match >= 0 && row >= 0 && row < split.rows {
		w.preview.row = row
	}
}

func (w *searchScreen) blurBlock() {
	w.previewFocused = false
}

// movePreview puts the cursor a number of rows down the transcript, or up it for a negative
// count, and brings the window round to it.
func (w *searchScreen) movePreview(m *model, delta int) tea.Cmd {
	split, ok := m.screenColumnSplit()
	if !ok {
		return nil
	}

	doc := &w.preview.doc
	line := max(min(w.previewLine(split.rows)+delta, len(doc.rows)-1), 0)
	doc.top = doc.shape().Reveal(line, split.rows)
	w.preview.row = line - doc.top

	return nil
}

// previewActions is the keys on the transcript while it holds them.
func (w *searchScreen) previewActions() screenActionSpec {
	return screenActionSpec{
		table: append(navActions(previewNav{w}, navSteps{page: navPageStep, scroll: previewWheelStep}), []screenAction{
			{
				id:   actionSearchOpen,
				keys: func(config.Keybindings) []string { return []string{"enter"} },
				nav:  func(m *model, _ string) tea.Cmd { return w.jumpToAgent(m) },
			},
			{
				id:   actionSearchBack,
				keys: fixedBack,
				nav: func(*model, string) tea.Cmd {
					w.blurBlock()

					return nil
				},
			},
			{
				id:   actionQuit,
				keys: fixedQuit,
				nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
			},
		}...),
		legend: []screenLegendRow{
			navMoveRow("move"),
			navPageRow(),
			navJumpRow(),
			{
				of:    []actionID{actionSearchOpen},
				desc:  func(*model) string { return "go to it" },
				strip: true,
			},
			{
				of:    []actionID{actionSearchBack},
				desc:  func(*model) string { return "back to the list" },
				strip: true,
			},
		},
	}
}

// previewLine is the row of the transcript the cursor is on, kept inside the window and the rows.
func (w *searchScreen) previewLine(height int) int {
	doc := w.preview.doc

	return max(min(doc.top+w.preview.row, doc.top+height-1, len(doc.rows)-1), 0)
}

// previewNav is the movement keys walking the transcript beside the results.
type previewNav struct{ w *searchScreen }

func (p previewNav) moveBy(m *model, rows int) tea.Cmd {
	return p.w.movePreview(m, rows)
}

func (p previewNav) moveToEnd(m *model, direction int) tea.Cmd {
	return p.w.movePreview(m, direction*len(p.w.preview.doc.rows))
}
