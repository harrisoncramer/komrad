package ui

import (
	"time"

	"weave/internal/ui/paint"
	"weave/internal/view"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
)

// blankCursor is the room a row leaves the arrow while the blink has it hidden.
const blankCursor = "  "

// rowCursorGlyph is the arrow a list draws on the row the cursor is standing on.
var rowCursorGlyph = view.Spell(view.GlyphFocus) + " "

// newRowCursor is the blink the lists share, focused from the start.
func newRowCursor() cursor.Model {
	c := cursor.New()
	c.BlinkSpeed = 300 * time.Millisecond
	_ = c.Focus()

	return c
}

// updateRowCursor carries the bubble's own blink timer, and puts the arrow back on for a
// keypress.
func (m model) updateRowCursor(msg tea.Msg) (model, tea.Cmd) {
	// The bubble stops its timer on a blur, and the working bars pulse on that timer whether or
	// not the panel has the keyboard, so the caret alone reads blurred.
	switch msg.(type) {
	case tea.BlurMsg, tea.FocusMsg:
		return m, nil
	}

	next, cmd := m.rowCursor.Update(msg)
	m.rowCursor = next

	// A model built without newRowCursor has no timer context, which the bubble's own restart
	// dereferences, so a cursor that was never made keeps whichever phase it is in.
	if _, ok := msg.(tea.KeyMsg); ok && m.rowCursor.BlinkSpeed > 0 {
		m.rowCursor.Blink = false

		return m, tea.Batch(cmd, m.rowCursor.BlinkCmd())
	}

	return m, cmd
}

// isCursorBlinkedOut reports whether the blink has the row cursor out of sight this frame.
func (m model) isCursorBlinkedOut(focused bool) bool {
	return focused && !m.isTyping() && m.rowCursor.Blink
}

// sidebarSection is one part of the panel's own pane that can hold the keys.
type sidebarSection int

const (
	sectionList sidebarSection = iota
	sectionRecord
	sectionTabs
	sectionMessages
	sectionForm
)

// keyholder is the section that holds the keys, or would if the keyboard were on the panel.
func (m model) keyholder() sidebarSection {
	switch {
	case m.form.isActive():
		return sectionForm
	case m.isMessagesFocused():
		return sectionMessages
	case m.isTabsFocused():
		return sectionTabs
	case m.isDetailsFocused():
		return sectionRecord
	}

	return sectionList
}

// isSectionLit reports whether a section draws its caret and its accent.
func (m model) isSectionLit(section sidebarSection) bool {
	return m.keyholder() == section
}

// isSectionHeld reports whether a section has the keyboard itself.
func (m model) isSectionHeld(section sidebarSection) bool {
	return !m.keysElsewhere && m.isSectionLit(section)
}

// cursorPrefix is the arrow a section draws on its hovered row, blank while the keys are in
// another section of the panel.
func (m model) cursorPrefix(section sidebarSection) string {
	if !m.isSectionLit(section) || m.isCursorBlinkedOut(m.isSectionHeld(section)) {
		return blankCursor
	}

	return paint.Panel.Cursor().Render(rowCursorGlyph)
}

func (m model) rowCursorPrefix() string {
	return m.cursorPrefix(sectionList)
}

// isListFocused reports whether the keys are the list's.
func (m model) isListFocused() bool {
	return m.isSectionHeld(sectionList)
}

// isColumnFocused reports whether the block beside the list holds the keys.
func (m model) isColumnFocused() bool {
	return m.isListFocused() && !m.isReviewThreadFocused()
}
