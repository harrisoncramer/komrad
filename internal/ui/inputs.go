package ui

import "komrad/internal/ui/screen"

// inputs is what the root hands the screen that is up.
func (m *model) inputs() screen.Inputs {
	return m.inputsFor(m.active())
}

// inputsFor is what the root hands a screen, whether it is up or not.
func (m *model) inputsFor(s screenModel) screen.Inputs {
	return screen.Inputs{
		Listing:     m.listing,
		Holds:       m.edited,
		Inspections: m.inspections,
		Up:          s == m.active(),
	}
}
