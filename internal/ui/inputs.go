package ui

import "komrad/internal/ui/screen"

// inputs is what the root hands the screen that is up.
func (m *model) inputs() screen.Inputs {
	return m.inputsFor(m.active())
}

// inputsFor is what the root hands a screen, whether it is up or not, with each stage of the room
// built from the stages before it.
func (m *model) inputsFor(s screenModel) screen.Inputs {
	in := m.measuredInputsFor(s)
	in.Room.ListFocused = m.isListFocused(in)
	in.Room.Column = m.columnSplit(in)
	in.Room.Split = m.screenColumnSplit(in)
	in.Room.Doc = m.docReaderOf(in)
	in.Room.Doc.Window = m.docWindowOf(in)
	in.Room.Caret = m.listCaret(in)

	return in
}

// measuredInputsFor is the inputs as far as the room's width and geometry, which is all the root
// needs to lay its own chrome out.
func (m *model) measuredInputsFor(s screenModel) screen.Inputs {
	in := screen.Inputs{
		Listing:     m.listing,
		Inspections: m.inspections,
		Up:          s == m.active(),
		Room: screen.Room{
			Height:          m.term.height,
			MessagesFocused: m.isMessagesFocused(),
			PopupForm:       m.isPopupForm() && m.form.isActive(),
			Dark:            m.term.isDark,
			Wrap:            m.diffWrap,
			HelpExpanded:    m.helpExpanded,
			Spinner:         m.loading.frame(),
			Now:             m.clock.Now(),
			Marks:           m.term.markingZones,
			Halved:          m.term.paneMeasure > 0,
			Pattern:         m.filter.Pattern(),
		},
	}
	in.Room.Geometry = m.columnGeometry(in)
	in.Room.Width = m.paneWidthAt(in.Room.Geometry)

	return in
}
