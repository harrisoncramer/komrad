package schedules

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

const (
	actionBack keys.ID = keys.Own + iota
	actionRename
	actionArchiveBack
)

// actions is the schedules screen as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func (w *Screen) actions(in screen.Inputs) keys.Spec[*Screen] {
	list := w.list()
	standing := func(*Screen) bool { return in.Room.Pattern.IsStanding() }

	return keys.Spec[*Screen]{
		Table: append([]keys.Action[*Screen]{
			{
				ID:   keys.FilterOpen,
				Keys: func(config.Keybindings) []string { return []string{filter.OpenKey} },
				Nav:  func(*Screen, string) screen.Request { return screen.BeginFilter{} },
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(*Screen, string) screen.Request { return screen.Hide{} },
			},
			{
				ID:   keys.FilterMatch,
				Keys: func(k config.Keybindings) []string { return slices.Concat(k.Filter.NextMatch, k.Filter.PrevMatch) },
				When: standing,
				Nav: func(_ *Screen, pressed string) screen.Request {
					return screen.MoveToMatch{Direction: keys.DirectionOf(pressed, w.deps.Keys.Filter.PrevMatch)}
				},
			},
			{
				ID:   keys.FilterClear,
				Keys: keys.FixedBack,
				When: standing,
				Nav: func(*Screen, string) screen.Request {
					return screen.Batch{screen.ClearFilterRestoreCursor{}, screen.WaitForCursorRest{}}
				},
			},
			{
				ID:   actionBack,
				Keys: keys.FixedBack,
				Nav:  func(*Screen, string) screen.Request { return screen.FocusViewSwitcher{} },
			},
			{
				ID:     actionRename,
				Keys:   func(k config.Keybindings) []string { return k.Schedules.Rename },
				Direct: keys.Directly(keys.NotInMenu, func(*Screen, string) screen.Request { return w.openRename(in) }),
			},
		}, keys.NavActions[*Screen](keys.NavActionsParams{
			Keys:   w.deps.Keys,
			Nav:    list,
			In:     in,
			Page:   keys.NavPageStep,
			Scroll: 1,
		})...),
		Legend: []keys.LegendRow[*Screen]{
			{
				ActionIDs: []keys.ID{actionRename},
				Desc:      func(*Screen) string { return "rename" },
			},
			{
				ActionIDs: []keys.ID{keys.FilterOpen},
				Desc:      keys.FilterDescOf[*Screen](in),
			},
			{
				ActionIDs: []keys.ID{keys.FilterMatch},
				Desc:      func(*Screen) string { return "next/prev match" },
			},
			{
				ActionIDs: []keys.ID{keys.Move},
				Desc:      func(*Screen) string { return "move" },
				Label:     keys.MoveLabel,
			},
			keys.NavPageRow[*Screen](),
			keys.JumpRow[*Screen](),
			{
				ActionIDs: []keys.ID{keys.ColumnScroll},
				Desc:      func(*Screen) string { return "scroll" },
				Label:     keys.ColumnScrollLabel,
			},
			{
				ActionIDs: []keys.ID{actionBack},
				Desc:      func(*Screen) string { return "back" },
			},
			{
				ActionIDs:     []keys.ID{keys.FilterClear},
				Desc:          keys.FilterClearDescOf[*Screen](in),
				IsInShortHelp: true,
			},
		},
	}
}
