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

// cursorPrefix is the arrow a list draws on its hovered row.
func (m model) cursorPrefix(focused bool) string {
	if m.isCursorBlinkedOut(focused) {
		return blankCursor
	}

	return paint.Panel.Cursor().Render(rowCursorGlyph)
}

func (m model) rowCursorPrefix() string {
	return m.cursorPrefix(m.isListFocused())
}

// isListFocused reports whether the keys are the list's.
func (m model) isListFocused() bool {
	return !m.keysElsewhere && !m.isDetailsFocused() && !m.isTabsFocused() && !m.isMessagesFocused() && !m.form.isActive()
}

// isColumnFocused reports whether the block beside the list holds the keys.
func (m model) isColumnFocused() bool {
	return m.isListFocused() && !m.isReviewThreadFocused()
}
