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
	standing := func(s panelShared) bool { return s.chrome.filter.isStanding() }

	return screenActionSpec{
		table: []screenAction{
			{
				id:   actionFilterOpen,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				run: func(h host, _ panelShared, _ string) tea.Cmd {
					h.beginFilter()

					return nil
				},
			},
			{
				id:   actionQuit,
				keys: list.quit,
				run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.hide() },
			},
			{
				id:   actionFilterMatch,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				when: standing,
				run:  func(h host, _ panelShared, pressed string) tea.Cmd { return h.jumpToFilterMatch(pressed) },
			},
			{
				id:   actionFilterClear,
				keys: list.back,
				when: standing,
				run: func(h host, _ panelShared, _ string) tea.Cmd {
					h.clearFilter()

					return h.waitToStage()
				},
			},
			{
				id:   actionScheduleBack,
				keys: list.back,
				run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.pickView() },
			},
			{
				id:   actionMove,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
				run:  list.movePressed,
			},
			{
				id:   actionNavPage,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
				run:  list.pagePressed,
			},
			{
				id:   actionColumnScroll,
				keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
				run:  list.scrollPressed,
			},
			{
				id:   actionScheduleNew,
				keys: func(k config.Keybindings) []string { return k.Schedules.New },
				run:  func(h host, s panelShared, _ string) tea.Cmd { return w.newForm(h, s) },
			},
			{
				id:   actionScheduleEdit,
				keys: func(k config.Keybindings) []string { return k.Schedules.Edit },
				run:  func(h host, s panelShared, _ string) tea.Cmd { return w.editForm(h, s) },
			},
			{
				id:   actionScheduleRename,
				keys: func(k config.Keybindings) []string { return k.Schedules.Rename },
				run:  func(_ host, s panelShared, _ string) tea.Cmd { return w.openRename(s) },
			},
			{
				id:   actionScheduleToggle,
				keys: func(k config.Keybindings) []string { return k.Schedules.Toggle },
				run:  func(h host, s panelShared, _ string) tea.Cmd { return w.toggle(h, s) },
			},
		},
		legend: []screenLegendRow{
			{
				of:   []actionID{actionScheduleNew},
				desc: func(panelShared) string { return "new schedule" },
			},
			{
				of:    []actionID{actionScheduleEdit},
				desc:  func(panelShared) string { return "edit" },
				strip: true,
			},
			{
				of:   []actionID{actionScheduleRename},
				desc: func(panelShared) string { return "rename" },
			},
			{
				of:   []actionID{actionScheduleToggle},
				desc: w.toggleDesc,
			},
			{
				of:   []actionID{actionFilterOpen},
				desc: filterDescOf,
			},
			{
				of:   []actionID{actionFilterMatch},
				desc: func(panelShared) string { return "next/prev match" },
			},
			{
				of:    []actionID{actionMove},
				desc:  func(panelShared) string { return "move" },
				label: moveLabel,
			},
			{
				of:    []actionID{actionNavPage},
				desc:  func(panelShared) string { return "page" },
				label: navPageLabel,
			},
			{
				of:    []actionID{actionColumnScroll},
				desc:  func(panelShared) string { return "scroll" },
				label: columnScrollLabel,
			},
			{
				of:   []actionID{actionScheduleBack},
				desc: func(panelShared) string { return "back" },
			},
			{
				of:    []actionID{actionFilterClear},
				desc:  filterClearDescOf,
				strip: true,
			},
		},
	}
}

func (w *schedulesScreen) toggleDesc(s panelShared) string {
	if item, ok := w.selected(s); ok && !item.schedule.Enabled {
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

func columnScrollLabel(k config.Keybindings) string {
	return keyLabel(k.Column.ScrollDown) + "/" + keyLabel(k.Column.ScrollUp)
}
