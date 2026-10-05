package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the schedules screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func (w *schedulesScreen) actions() screenActionSpec {
	list := w.list()
	standing := func(m *model) bool { return m.filter.isStanding() }

	return screenActionSpec{
		table: append([]screenAction{
			{
				id:   actionFilterOpen,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				nav: func(m *model, _ string) tea.Cmd {
					m.beginFilter()

					return nil
				},
			},
			{
				id:   actionQuit,
				keys: func(config.Keybindings) []string { return fixedKeys().Quit },
				nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
			},
			{
				id:   actionFilterMatch,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				when: standing,
				nav:  func(m *model, pressed string) tea.Cmd { return m.jumpToFilterMatch(pressed) },
			},
			{
				id:   actionFilterClear,
				keys: func(config.Keybindings) []string { return fixedKeys().Back },
				when: standing,
				nav: func(m *model, _ string) tea.Cmd {
					m.clearFilter()

					return m.waitToStage()
				},
			},
			{
				id:   actionScheduleBack,
				keys: func(config.Keybindings) []string { return fixedKeys().Back },
				nav:  func(m *model, _ string) tea.Cmd { return m.pickView() },
			},
			{
				id:     actionScheduleRename,
				keys:   func(k config.Keybindings) []string { return k.Schedules.Rename },
				direct: directly(notInMenu, func(m *model, _ string) tea.Cmd { return w.openRename(m) }),
			},
		}, navActions(list, navSteps{page: navPageStep, scroll: 1})...),
		legend: []screenLegendRow{
			{
				of:   []actionID{actionScheduleRename},
				desc: func(*model) string { return "rename" },
			},
			{
				of:   []actionID{actionFilterOpen},
				desc: filterDescOf,
			},
			{
				of:   []actionID{actionFilterMatch},
				desc: func(*model) string { return "next/prev match" },
			},
			{
				of:    []actionID{actionMove},
				desc:  func(*model) string { return "move" },
				label: moveLabel,
			},
			navPageRow(),
			navJumpRow(),
			{
				of:    []actionID{actionColumnScroll},
				desc:  func(*model) string { return "scroll" },
				label: columnScrollLabel,
			},
			{
				of:   []actionID{actionScheduleBack},
				desc: func(*model) string { return "back" },
			},
			{
				of:    []actionID{actionFilterClear},
				desc:  filterClearDescOf,
				strip: true,
			},
		},
	}
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

func columnScrollLabel(k config.Keybindings) string {
	return keyLabel(k.Column.ScrollDown) + "/" + keyLabel(k.Column.ScrollUp)
}
