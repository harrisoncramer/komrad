package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"

	tea "charm.land/bubbletea/v2"
)

// actions is the schedules screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func (w *schedulesScreen) actions() keys.Spec[*model] {
	list := w.list()
	standing := func(m *model) bool { return m.filter.isStanding() }

	return keys.Spec[*model]{
		Table: append([]keys.Action[*model]{
			{
				ID:   keys.FilterOpen,
				Keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				Nav: func(m *model, _ string) tea.Cmd {
					m.beginFilter()

					return nil
				},
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
			},
			{
				ID:   keys.FilterMatch,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				When: standing,
				Nav:  func(m *model, pressed string) tea.Cmd { return m.jumpToFilterMatch(pressed) },
			},
			{
				ID:   keys.FilterClear,
				Keys: keys.FixedBack,
				When: standing,
				Nav: func(m *model, _ string) tea.Cmd {
					m.clearFilter()

					return m.waitToStage()
				},
			},
			{
				ID:   actionScheduleBack,
				Keys: keys.FixedBack,
				Nav:  func(m *model, _ string) tea.Cmd { return m.pickView() },
			},
			{
				ID:     actionScheduleRename,
				Keys:   func(k config.Keybindings) []string { return k.Schedules.Rename },
				Direct: keys.Directly(keys.NotInMenu, func(m *model, _ string) tea.Cmd { return w.openRename(m) }),
			},
		}, navActions(list, navSteps{page: navPageStep, scroll: 1})...),
		Legend: []keys.LegendRow[*model]{
			{
				Of:   []keys.ID{actionScheduleRename},
				Desc: func(*model) string { return "rename" },
			},
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: filterDescOf,
			},
			{
				Of:   []keys.ID{keys.FilterMatch},
				Desc: func(*model) string { return "next/prev match" },
			},
			{
				Of:    []keys.ID{keys.Move},
				Desc:  func(*model) string { return "move" },
				Label: keys.MoveLabel,
			},
			keys.NavPageRow[*model](),
			keys.JumpRow[*model](),
			{
				Of:    []keys.ID{keys.ColumnScroll},
				Desc:  func(*model) string { return "scroll" },
				Label: keys.ColumnScrollLabel,
			},
			{
				Of:   []keys.ID{actionScheduleBack},
				Desc: func(*model) string { return "back" },
			},
			{
				Of:    []keys.ID{keys.FilterClear},
				Desc:  filterClearDescOf,
				Strip: true,
			},
		},
	}
}
