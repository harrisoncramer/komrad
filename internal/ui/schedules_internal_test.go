package ui

// scheduleHelp is the schedules legend as the frame draws it.
func (m model) scheduleHelp() keyMap {
	return m.listHelpOf(m.schedules.listParams(m.shared()))
}
