package ui

import (
	"slices"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

// actions is the archived schedules as one table and the legend that names it.
func (a *archivedSchedules) actions(in screen.Inputs) keys.Spec[*archivedSchedules] {
	return keys.Spec[*archivedSchedules]{
		Table:  a.table(in),
		Legend: a.legend(in),
	}
}

func (a *archivedSchedules) table(in screen.Inputs) []keys.Action[*archivedSchedules] {
	list := a.list()

	return append(keys.ListActions[*archivedSchedules](list, in),
		keys.Action[*archivedSchedules]{
			ID:   actionArchiveBack,
			Keys: keys.FixedBack,
			Nav:  func(*archivedSchedules, string) screen.Request { return screen.PickView{} },
		},
	)
}

func (a *archivedSchedules) legend(in screen.Inputs) []keys.LegendRow[*archivedSchedules] {
	return slices.Concat([]keys.LegendRow[*archivedSchedules]{
		{
			Of:   []keys.ID{keys.FilterOpen},
			Desc: keys.FilterDescOf[*archivedSchedules](in),
		},
	}, keys.NavLegend[*archivedSchedules](), []keys.LegendRow[*archivedSchedules]{
		{
			Of:   []keys.ID{actionArchiveBack},
			Desc: func(*archivedSchedules) string { return "back" },
		},
		{
			Of:    []keys.ID{keys.FilterClear},
			Desc:  keys.FilterClearDescOf[*archivedSchedules](in),
			Strip: true,
		},
		{
			Of:    []keys.ID{keys.FilterMatch},
			Desc:  func(*archivedSchedules) string { return "next/prev match" },
			Strip: true,
		},
	})
}
