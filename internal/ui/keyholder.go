package ui

import (
	"komrad/internal/ui/screen"
	"komrad/internal/view"
)

// sidebarSection is one of the sections of the panel's own pane that has a cursor of its own.
type sidebarSection int

const (
	sectionList sidebarSection = iota
	sectionRecord
	sectionTabs
	sectionKinds
	sectionViews
)

// keyholder is the section whose cursor the arrow keys would move.
func (m *model) keyholder(in screen.Inputs) sidebarSection {
	if m.isViewsOpen() {
		return sectionViews
	}

	return m.keyholderBehindViews(in)
}

func (m *model) keyholderBehindViews(in screen.Inputs) sidebarSection {
	switch {
	case m.isTabsFocused(in):
		return sectionTabs
	case m.isKindsFocused(in):
		return sectionKinds
	case m.isDetailsFocused(in):
		return sectionRecord
	}

	return sectionList
}

// doKeysMoveCursors reports whether the keyboard is on a sidebar cursor at all.
func (m *model) doKeysMoveCursors() bool {
	return !m.term.keysElsewhere && !m.form.isActive() && !m.isMessagesFocused()
}

// caretOf is how a section's arrow stands this frame.
func (m *model) caretOf(in screen.Inputs, section sidebarSection) view.Caret {
	if m.keyholderBehindViews(in) != section {
		return view.CaretAbsent
	}

	return view.CaretRendered
}

// listCaret is how the list's arrow stands, which is rendered while the document beside it holds
// the keys.
func (m *model) listCaret(in screen.Inputs) view.Caret {
	if in.Room.Doc.Focused {
		return view.CaretRendered
	}

	return m.caretOf(in, sectionList)
}

// isListFocused reports whether the keys are the list's.
func (m *model) isListFocused(in screen.Inputs) bool {
	return m.doKeysMoveCursors() && m.keyholder(in) == sectionList
}
