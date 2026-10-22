package ui

import (
	"komrad/internal/ui/list"
	"komrad/internal/ui/screen"
	"komrad/internal/view"
)

// keyholder is the section whose cursor the arrow keys would move.
func (m *model) keyholder(in screen.Inputs) list.Section {
	if m.isViewsOpen() {
		return list.SectionViews
	}

	return m.keyholderBehindViews(in)
}

func (m *model) keyholderBehindViews(in screen.Inputs) list.Section {
	switch {
	case m.isTabsFocused(in):
		return list.SectionTabs
	case m.isKindsFocused(in):
		return list.SectionKinds
	case m.isDetailsFocused(in):
		return list.SectionRecord
	}

	return list.SectionList
}

// doKeysMoveCursors reports whether the keyboard is on a sidebar cursor at all.
func (m *model) doKeysMoveCursors() bool {
	return !m.term.keysElsewhere && !m.form.isActive() && !m.isMessagesFocused()
}

// caretOf is how a section's arrow stands this frame.
func (m *model) caretOf(in screen.Inputs, section list.Section) view.Caret {
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

	return m.caretOf(in, list.SectionList)
}

// isListFocused reports whether the keys are the list's.
func (m *model) isListFocused(in screen.Inputs) bool {
	return m.doKeysMoveCursors() && m.keyholder(in) == list.SectionList
}
