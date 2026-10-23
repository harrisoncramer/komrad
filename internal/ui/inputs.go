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
	in.Room.IsListFocused = m.isListFocused(in)
	in.Room.BlockAreaSplit = m.blockAreaSplitOf(in)
	in.Room.OwnBlockSplit = m.ownBlockSplitOf(in)
	in.Room.DocumentReader = m.docReaderOf(in)
	in.Room.DocumentReader.VisibleRows = m.docVisibleRowsOf(in)
	in.Room.ListCaret = m.listCaret(in)

	return in
}

// measuredInputsFor is the inputs as far as the room's width and window split, which is all
// the root needs to lay its own chrome out.
func (m *model) measuredInputsFor(s screenModel) screen.Inputs {
	in := screen.Inputs{
		Listing:             m.listing,
		WorktreeInspections: m.inspections,
		IsActive:            s == m.active(),
		Room: screen.Room{
			PaneHeight:         m.term.height,
			AreMessagesFocused: m.isMessagesFocused(),
			IsFormInPopup:      m.isPopupForm() && m.form.isActive(),
			IsDarkBackground:   m.term.isDark,
			IsDiffWrapped:      m.diffWrap,
			IsHelpExpanded:     m.helpExpanded,
			SpinnerFrame:       m.loading.frame(),
			Now:                m.clock.Now(),
			ShouldMarkZones:    m.term.markingZones,
			IsListHalfOfSplit:  m.term.paneMeasure > 0,
			Pattern:            m.filter.Pattern(),
		},
	}
	in.Room.WindowSplit = m.windowSplitOf(in)
	in.Room.DrawingWidth = m.drawingWidthAt(in.Room.WindowSplit)

	return in
}
