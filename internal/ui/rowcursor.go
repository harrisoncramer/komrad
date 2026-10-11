package ui

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/view"
)

// blankCursor is the room a row leaves where the arrow would stand.
const blankCursor = "  "

// rowCursorGlyph is the arrow a list renders on the row the cursor is standing on.
var rowCursorGlyph = glyph.Focus + " "

// sidebarSection is one of the sections of the panel's own pane that has a cursor of its own.
type sidebarSection int

const (
	sectionList sidebarSection = iota
	sectionRecord
	sectionTabs
	sectionViews
)

// keyholder is the section whose cursor the arrow keys would move.
func (m *model) keyholder() sidebarSection {
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
func (m *model) doKeysMoveCursors() bool {
	return !m.term.keysElsewhere && !m.form.isActive() && !m.isMessagesFocused()
}

// caretOf is how a section's arrow stands this frame.
func (m *model) caretOf(section sidebarSection) view.Caret {
	if m.keyholder() != section {
		return view.CaretAbsent
	}

	return view.CaretRendered
}

// cursorPrefix is the arrow a section renders on its hovered row.
func (m *model) cursorPrefix(section sidebarSection) string {
	return caretGlyph(m.caretOf(section))
}

func caretGlyph(caret view.Caret) string {
	if caret != view.CaretRendered {
		return blankCursor
	}

	return paint.Panel.Cursor().Render(rowCursorGlyph)
}

// isListFocused reports whether the keys are the list's.
func (m *model) isListFocused() bool {
	return m.doKeysMoveCursors() && m.keyholder() == sectionList
}
