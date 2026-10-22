package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

// actions is the schedules screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func (w *schedulesScreen) actions() keys.Spec[*model] {
	list := w.list()
	standing := func(m *model) bool { return m.inputs().Room.Pattern.IsStanding() }

	return keys.Spec[*model]{
		Table: append([]keys.Action[*model]{
			{
				ID:   keys.FilterOpen,
				Keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				Nav:  func(*model, string) screen.Request { return screen.BeginFilter{} },
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(m *model, _ string) screen.Request { return screen.Hide{} },
			},
			{
				ID:   keys.FilterMatch,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				When: standing,
				Nav: func(m *model, pressed string) screen.Request {
					return screen.StepMatch{Delta: keys.DirectionOf(pressed, w.deps.keys.Filter.PrevMatch)}
				},
			},
			{
				ID:   keys.FilterClear,
				Keys: keys.FixedBack,
				When: standing,
				Nav:  func(*model, string) screen.Request { return screen.Batch{screen.ClearFilter{}, screen.Settle{}} },
			},
			{
				ID:   actionScheduleBack,
				Keys: keys.FixedBack,
				Nav:  func(m *model, _ string) screen.Request { return screen.PickView{} },
			},
			{
				ID:     actionScheduleRename,
				Keys:   func(k config.Keybindings) []string { return k.Schedules.Rename },
				Direct: keys.Directly(keys.NotInMenu, func(m *model, _ string) screen.Request { return w.openRename(m, m.inputs()) }),
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
