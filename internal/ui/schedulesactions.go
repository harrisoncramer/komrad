package ui

import (
	"weave/config"

	tea "github.com/charmbracelet/bubbletea"
)

// schedulesActions is the schedules screen as one table, walked in precedence order by the
// dispatch and named in reading order by a legend of the same ids.
func schedulesActions() actionSpec {
	return actionSpec{
		table: []action{
			{
				id:   actionFilterOpen,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openFilter() },
			},
			{
				id:   actionQuit,
				keys: func(k config.Keybindings) []string { return k.Schedules.Quit },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m, m.hide() },
			},
			{
				id:   actionFilterMatch,
				keys: func(k config.Keybindings) []string { return concatKeys(k.Filter.NextMatch, k.Filter.PrevMatch) },
				when: func(m model) bool { return m.filter.isStanding() },
				run:  model.jumpFilterMatch,
			},
			{
				id:   actionFilterClear,
				keys: func(k config.Keybindings) []string { return k.Schedules.Back },
				when: func(m model) bool { return m.filter.isStanding() },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.clearSearch() },
			},
			{
				id:   actionScheduleBack,
				keys: func(k config.Keybindings) []string { return k.Schedules.Back },
				run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.returnToAgents() },
			},
			{
				id:   actionMove,
				keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Up, k.Nav.Down) },
				run:  moveSchedules,
			},
			{
				id:   actionNavPage,
				keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.PageUp, k.Nav.PageDown) },
				run: func(m model, pressed string) (tea.Model, tea.Cmd) {
					m.schedules.jump(&m, m.shared(), m.schedules.cursor+m.navPageDelta(pressed))

					return m, nil
				},
			},
			{
				id:   actionScheduleNew,
				keys: func(k config.Keybindings) []string { return k.Schedules.New },
				run: func(m model, _ string) (tea.Model, tea.Cmd) {
					cmd := m.schedules.newForm(&m, m.shared())

					return m, cmd
				},
			},
			{
				id:   actionScheduleEdit,
				keys: func(k config.Keybindings) []string { return k.Schedules.Edit },
				run: func(m model, _ string) (tea.Model, tea.Cmd) {
					cmd := m.schedules.editForm(&m, m.shared())

					return m, cmd
				},
			},
			{
				id:   actionScheduleToggle,
				keys: func(k config.Keybindings) []string { return k.Schedules.Toggle },
				run: func(m model, _ string) (tea.Model, tea.Cmd) {
					cmd := m.schedules.toggle(&m, m.shared())

					return m, cmd
				},
			},
			{
				id:   actionScheduleArchive,
				keys: func(k config.Keybindings) []string { return k.Schedules.Archive },
				run: func(m model, _ string) (tea.Model, tea.Cmd) {
					cmd := m.schedules.archiveConfirm(&m, m.shared())

					return m, cmd
				},
			},
		},
		legend: []legendRow{
			{
				of:   []actionID{actionScheduleNew},
				desc: func(model) string { return "new schedule" },
			},
			{
				of:    []actionID{actionScheduleEdit},
				desc:  func(model) string { return "edit" },
				strip: true,
			},
			{
				of:   []actionID{actionScheduleToggle},
				desc: scheduleToggleDesc,
			},
			{
				of:   []actionID{actionScheduleArchive},
				desc: func(model) string { return "archive" },
			},
			{
				of:   []actionID{actionFilterOpen},
				desc: model.filterDesc,
			},
			{
				of:   []actionID{actionFilterMatch},
				desc: func(model) string { return "next/prev match" },
			},
			{
				of:    []actionID{actionMove},
				desc:  func(model) string { return "move" },
				label: moveLabel,
			},
			{
				of:    []actionID{actionNavPage},
				desc:  func(model) string { return "page" },
				label: navPageLabel,
			},
			{
				of:   []actionID{actionScheduleBack},
				desc: func(model) string { return "back" },
			},
			{
				of:    []actionID{actionFilterClear},
				desc:  model.filterClearDesc,
				strip: true,
			},
		},
	}
}

// moveSchedules steps the cursor, and a step refused at the bottom walks on into the record.
func moveSchedules(m model, pressed string) (tea.Model, tea.Cmd) {
	delta := -1
	if isBound(pressed, m.keys.Nav.Down) {
		delta = 1
	}

	if !m.schedules.move(&m, m.shared(), delta) && delta > 0 {
		return m.walkIntoDetails()
	}

	return m, nil
}

func scheduleToggleDesc(m model) string {
	if item, ok := m.schedules.selected(m.shared()); ok && !item.schedule.Enabled {
		return "resume"
	}

	return "pause"
}

func jumpLabel(k config.Keybindings) string {
	return doubledLabel(k.Nav.Top) + "/" + keyLabel(k.Nav.Bottom)
}

func moveLabel(k config.Keybindings) string {
	return keyLabel(k.Nav.Up) + "/" + keyLabel(k.Nav.Down)
}

func navPageLabel(k config.Keybindings) string {
	return keyLabel(k.Nav.PageUp) + "/" + keyLabel(k.Nav.PageDown)
}
