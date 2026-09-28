package ui

import (
	"time"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/view"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"
)

// blankCursor is the room a row leaves the arrow while the blink has it hidden.
const blankCursor = "  "

// rowCursorGlyph is the arrow a list draws on the row the cursor is standing on.
var rowCursorGlyph = glyph.Focus + " "

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
	if _, ok := msg.(tea.KeyPressMsg); ok && m.rowCursor.BlinkSpeed > 0 {
		m.rowCursor.IsBlinked = false

		return m, tea.Batch(cmd, m.rowCursor.Blink())
	}

	return m, cmd
}

// focusPalette is the panel's palette on the blink's beat, for whatever holds the keys.
func (m model) focusPalette() paint.Palette {
	return paint.Panel.Blinking(m.rowCursor.IsBlinked)
}

// isCursorBlinkedOut reports whether the blink has the row cursor out of sight this frame.
func (m model) isCursorBlinkedOut(focused bool) bool {
	return focused && !m.isTyping() && m.rowCursor.IsBlinked
}

// sidebarSection is one of the sections of the panel's own pane that has a cursor of its own.
type sidebarSection int

const (
	sectionList sidebarSection = iota
	sectionRecord
	sectionTabs
	sectionViews
)

// keyholder is the section whose cursor the arrow keys would move.
func (m model) keyholder() sidebarSection {
	switch {
	case m.isViewsOpen():
		return sectionViews
	case m.isTabsFocused(), m.isKindsFocused():
		return sectionTabs
	case m.isDetailsFocused():
		return sectionRecord
	}

	return sectionList
}

// doKeysMoveCursors reports whether the keyboard is on a sidebar cursor at all.
func (m model) doKeysMoveCursors() bool {
	return !m.keysElsewhere && !m.form.isActive() && !m.isMessagesFocused()
}

// caretOf is how a section's arrow stands this frame.
func (m model) caretOf(section sidebarSection) view.Caret {
	if m.keyholder() != section {
		return view.CaretAbsent
	}

	return m.blinkingCaret(m.doKeysMoveCursors())
}

// blinkingCaret is the arrow as the blink leaves it on a cursor the keyboard is or is not on.
func (m model) blinkingCaret(held bool) view.Caret {
	if m.isCursorBlinkedOut(held) {
		return view.CaretBlinkedOut
	}

	return view.CaretDrawn
}

// cursorPrefix is the arrow a section draws on its hovered row.
func (m model) cursorPrefix(section sidebarSection) string {
	return caretGlyph(m.caretOf(section))
}

func caretGlyph(caret view.Caret) string {
	if caret != view.CaretDrawn {
		return blankCursor
	}

	return paint.Panel.Cursor().Render(rowCursorGlyph)
}

// isListFocused reports whether the keys are the list's.
func (m model) isListFocused() bool {
	return m.doKeysMoveCursors() && m.keyholder() == sectionList
}
