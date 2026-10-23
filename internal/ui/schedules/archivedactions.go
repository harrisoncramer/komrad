package schedules

import (
	"slices"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

// actions is the archived schedules as one table and the legend that names it.
func (a Archive) actions(in screen.Inputs) keys.Spec[Archive] {
	return keys.Spec[Archive]{
		Table:  a.table(in),
		Legend: a.legend(in),
	}
}

func (a Archive) table(in screen.Inputs) []keys.Action[Archive] {
	list := a.list()

	return append(keys.ListActions[Archive](list, in),
		keys.Action[Archive]{
			ID:   actionArchiveBack,
			Keys: keys.FixedBack,
			Nav:  func(Archive, string) screen.Request { return screen.FocusViewSwitcher{} },
		},
	)
}

func (a Archive) legend(in screen.Inputs) []keys.LegendRow[Archive] {
	return slices.Concat([]keys.LegendRow[Archive]{
		{
			ActionIDs: []keys.ID{keys.FilterOpen},
			Desc:      keys.FilterDescOf[Archive](in),
		},
	}, keys.NavLegend[Archive](), []keys.LegendRow[Archive]{
		{
			ActionIDs: []keys.ID{actionArchiveBack},
			Desc:      func(Archive) string { return "back" },
		},
		{
			ActionIDs: []keys.ID{keys.FilterClear},
			Desc:      keys.FilterClearDescOf[Archive](in),
			Strip:     true,
		},
		{
			ActionIDs: []keys.ID{keys.FilterMatch},
			Desc:      func(Archive) string { return "next/prev match" },
			Strip:     true,
		},
	})
}
