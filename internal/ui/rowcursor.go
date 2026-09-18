package ui

import (
	"weave/internal/ui/paint"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
)

// rowCursorGlyph is the arrow a list draws on the row the cursor is standing on, and blankCursor
// is the room it leaves while the blink has it hidden.
const (
	rowCursorGlyph = "› "
	blankCursor    = "  "
)

// newRowCursor is the blink the lists share, focused from the start, since a cursor nothing has
// ticked yet would sit there dark.
func newRowCursor() cursor.Model {
	c := cursor.New()
	_ = c.Focus()

	return c
}

// updateRowCursor carries the bubble's own blink timer, and puts the arrow back on for a
// keypress so a row being moved to is drawn solid.
func (m model) updateRowCursor(msg tea.Msg) (model, tea.Cmd) {
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

// cursorBlinkedOut reports whether the blink has the row cursor out of sight this frame.
func (m model) cursorBlinkedOut(focused bool) bool {
	return focused && !m.blurred && !m.isTyping() && m.rowCursor.Blink
}

// cursorPrefix is the arrow a list draws on its hovered row, blinking while that list has the
// keyboard and steady while something is being typed into.
func (m model) cursorPrefix(focused bool) string {
	if m.cursorBlinkedOut(focused) {
		return blankCursor
	}

	return paint.Panel.Cursor().Render(rowCursorGlyph)
}

func (m model) rowCursorPrefix() string {
	return m.cursorPrefix(m.isListFocused())
}

// isListFocused reports whether the keys are the list's rather than another pane's, the record
// beside it or a form standing over it.
func (m model) isListFocused() bool {
	return !m.keysElsewhere && !m.isDetailsFocused() && !m.isMessagesFocused() && !m.form.isActive()
}

// isColumnFocused reports whether the block beside the list holds the keys, which it does with
// the list itself on every screen but the review, where the conversation can take them instead.
func (m model) isColumnFocused() bool {
	return m.isListFocused() && !m.isReviewThreadFocused()
}
