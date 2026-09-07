package ui

import (
	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
)

// rowCursorGlyph is the arrow a list draws on the row the cursor is standing on, and blankCursor
// is the room it leaves while the blink has it hidden.
const (
	rowCursorGlyph = "› "
	blankCursor    = "  "
)

// newRowCursor is the blink the lists share, focused from the start since a cursor nothing has
// ticked yet would sit there dark.
func newRowCursor() cursor.Model {
	c := cursor.New()
	_ = c.Focus()

	return c
}

// updateRowCursor carries the bubble's own blink timer, and puts the arrow back on for a keypress
// so a row being moved to is drawn solid rather than under whichever half of the blink the press
// landed in.
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

// cursorPrefix is the arrow a list draws on its hovered row, blinking while that list has the
// keyboard, steady while something is being typed into, and gone for the half of the phase the
// bubble sets Blink over.
func (m model) cursorPrefix(focused bool) string {
	if focused && !m.blurred && !m.isTyping() && m.rowCursor.Blink {
		return blankCursor
	}

	return cursorStyleFor(focused).Render(rowCursorGlyph)
}

func (m model) rowCursorPrefix() string {
	return m.cursorPrefix(m.isListFocused())
}

// isListFocused reports whether the keys are the list's rather than the record beside it or a
// form standing over it, which is what decides how the row under the cursor is drawn, arrow and
// name alike.
func (m model) isListFocused() bool {
	return !m.isDetailsFocused() && !m.isMessagesFocused() && !m.form.isActive()
}
