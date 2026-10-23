package ui

import (
	"komrad/internal/ui/field"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
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
	if m.screen == screen.Agents && m.listing.IsEmpty() {
		return gutter + m.term.width
	}

	if split := m.inputs().Room.Geometry; split.On {
		return gutter + split.Sidebar
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
		focused := m.form.rendered.GetFocusedField()

		return focused != nil && field.IsText(focused) && m.form.vim.IsInserting(focused.GetKey())
	}

	if m.screen == screen.Review && m.review.IsWriting() {
		return m.review.IsInserting()
	}

	if line, ok := m.typedLine(); ok {
		return line.IsInserting()
	}

	return m.isTyping()
}

// typedLine is the one-row field taking the keys, if one is.
func (m *model) typedLine() (field.Line, bool) {
	switch {
	case m.filter.IsTyping:
		return m.filter.Input, true
	case m.reading.filter.IsTyping && m.isDocReadable(m.inputs()):
		return m.reading.filter.Input, true
	}

	if typer, ok := m.active().(lineTyper); ok {
		return typer.TypedLine()
	}

	return field.Line{}, false
}
