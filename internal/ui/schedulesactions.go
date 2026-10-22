package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

// actions is the schedules screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func (w *schedulesScreen) actions(in screen.Inputs) keys.Spec[*schedulesScreen] {
	list := w.list()
	standing := func(*schedulesScreen) bool { return in.Room.Pattern.IsStanding() }

	return keys.Spec[*schedulesScreen]{
		Table: append([]keys.Action[*schedulesScreen]{
			{
				ID:   keys.FilterOpen,
				Keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				Nav:  func(*schedulesScreen, string) screen.Request { return screen.BeginFilter{} },
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(*schedulesScreen, string) screen.Request { return screen.Hide{} },
			},
			{
				ID:   keys.FilterMatch,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				When: standing,
				Nav: func(_ *schedulesScreen, pressed string) screen.Request {
					return screen.StepMatch{Delta: keys.DirectionOf(pressed, w.deps.keys.Filter.PrevMatch)}
				},
			},
			{
				ID:   keys.FilterClear,
				Keys: keys.FixedBack,
				When: standing,
				Nav: func(*schedulesScreen, string) screen.Request {
					return screen.Batch{screen.ClearFilter{}, screen.Settle{}}
				},
			},
			{
				ID:   actionScheduleBack,
				Keys: keys.FixedBack,
				Nav:  func(*schedulesScreen, string) screen.Request { return screen.PickView{} },
			},
			{
				ID:     actionScheduleRename,
				Keys:   func(k config.Keybindings) []string { return k.Schedules.Rename },
				Direct: keys.Directly(keys.NotInMenu, func(*schedulesScreen, string) screen.Request { return w.openRename(in) }),
			},
		}, navActions[*schedulesScreen](navActionsParams{keys: w.deps.keys, nav: list, in: in, page: navPageStep, scroll: 1})...),
		Legend: []keys.LegendRow[*schedulesScreen]{
			{
				Of:   []keys.ID{actionScheduleRename},
				Desc: func(*schedulesScreen) string { return "rename" },
			},
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: filterDescOf[*schedulesScreen](in),
			},
			{
				Of:   []keys.ID{keys.FilterMatch},
				Desc: func(*schedulesScreen) string { return "next/prev match" },
			},
			{
				Of:    []keys.ID{keys.Move},
				Desc:  func(*schedulesScreen) string { return "move" },
				Label: keys.MoveLabel,
			},
			keys.NavPageRow[*schedulesScreen](),
			keys.JumpRow[*schedulesScreen](),
			{
				Of:    []keys.ID{keys.ColumnScroll},
				Desc:  func(*schedulesScreen) string { return "scroll" },
				Label: keys.ColumnScrollLabel,
			},
			{
				Of:   []keys.ID{actionScheduleBack},
				Desc: func(*schedulesScreen) string { return "back" },
			},
			{
				Of:    []keys.ID{keys.FilterClear},
				Desc:  filterClearDescOf[*schedulesScreen](in),
				Strip: true,
			},
		},
	}
}
