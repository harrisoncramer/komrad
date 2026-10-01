package ui

import (
	"strings"

	"komrad/internal/ui/paint"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The labels naming the mode that holds the panel.
const (
	normalModeLabel = " NORMAL "
	visualModeLabel = " VISUAL "
	jumpModeLabel   = "  JUMP  "
)

// drawInMode is the sidebar inside its gutters with label drawn in tone at the right end of the
// sidebar's bottom row.
func (m model) drawInMode(tone paint.Tone, label string) string {
	lines := strings.Split(m.drawInset(m.insideView()), "\n")

	bottom := len(lines) - 1
	if m.height > 0 {
		bottom = min(bottom, m.height-1)
	}

	lines[bottom] = stampMode(lines[bottom], m.findModeEnd(), tone, label)

	return strings.Join(lines, "\n")
}

// findModeEnd is the column of the pane the mode's label ends at, the last of the sidebar's own
// cells short of a gutter, and never in the block drawn beside the sidebar.
func (m model) findModeEnd() int {
	if m.frame.IsSidebarOnRight() {
		return m.countInsetLeft() + m.width - gutter
	}

	if m.screen == screenAgents && m.listing.isEmpty() {
		return m.countInsetLeft() + m.width
	}

	if split, ok := m.columnGeometry(); ok {
		return m.countInsetLeft() + split.sidebar
	}

	return m.countInsetLeft() + m.width
}

// findMode is the tone and label of the mode the panel is in while it draws itself.
func (m model) findMode() (paint.Tone, string) {
	if m.paneCopying {
		return paint.ToneModeVisual, visualModeLabel
	}

	if m.isEditing() {
		return paint.ToneModeEdit, editModeLabel
	}

	return paint.ToneModeNormal, normalModeLabel
}

// stampMode draws label in tone over row, ending at the column end.
func stampMode(row string, end int, tone paint.Tone, label string) string {
	badge := paint.Panel.Mode(tone).Render(label)
	at := end - lipgloss.Width(badge)
	if at < 0 || end > ansi.StringWidth(row) {
		return row
	}

	return ansi.Truncate(row, at, "") + badge + ansi.Cut(row, end, ansi.StringWidth(row))
}

// isEditing reports whether a text field holds the keys and is taking typed text.
func (m model) isEditing() bool {
	if m.form.isActive() {
		focused := m.form.form.GetFocusedField()

		return focused != nil && isTextField(focused) && m.form.vim.isInserting(focused.GetKey())
	}

	if m.screen == screenReview && m.review.isWriting() {
		return m.review.threads.editor.isInserting()
	}

	return m.isTyping()
}
