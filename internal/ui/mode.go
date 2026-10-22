package ui

import (
	"komrad/internal/ui/field"
	"komrad/internal/ui/paint"
)

// The labels naming the mode that holds the panel.
const (
	normalModeLabel = " NORMAL "
	editModeLabel   = "  EDIT  "
	visualModeLabel = " VISUAL "
	jumpModeLabel   = "  JUMP  "
)

// findModeEnd is the column of the pane the mode's label ends at, the last of the sidebar's own
// cells short of a gutter, and never in the block rendered beside the sidebar.
func (m *model) findModeEnd() int {
	if m.screen == screenAgents && m.listing.isEmpty() {
		return gutter + m.term.width
	}

	if split, ok := m.columnGeometry(); ok {
		return gutter + split.sidebar
	}

	return gutter + m.term.width
}

// findMode is the tone and label of the mode the panel is in while it renders itself.
func (m *model) findMode() (paint.Tone, string) {
	if m.term.paneCopying {
		return paint.ToneModeVisual, visualModeLabel
	}

	if m.isEditing() {
		return paint.ToneModeEdit, editModeLabel
	}

	return paint.ToneModeNormal, normalModeLabel
}

// isEditing reports whether a text field holds the keys and is taking typed text.
func (m *model) isEditing() bool {
	if m.form.isActive() {
		focused := m.form.form.GetFocusedField()

		return focused != nil && field.IsText(focused) && m.form.vim.IsInserting(focused.GetKey())
	}

	if m.screen == screenReview && m.review.isWriting() {
		return m.review.threads.editor.isInserting()
	}

	if line, ok := m.typedLine(); ok {
		return line.IsInserting()
	}

	return m.isTyping()
}

// typedLine is the one-row field taking the keys, if one is.
func (m *model) typedLine() (field.Line, bool) {
	switch {
	case m.filter.typing:
		return m.filter.input, true
	case m.reading.filter.typing && m.isDocReadable():
		return m.reading.filter.input, true
	}

	if typer, ok := m.active().(lineTyper); ok {
		return typer.typedLine()
	}

	return field.Line{}, false
}
