package ui

import (
	"slices"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

// actions is the archived schedules as one table and the legend that names it.
func (a *archivedSchedules) actions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table:  a.table(),
		Legend: a.legend(),
	}
}

func (a *archivedSchedules) table() []keys.Action[*model] {
	list := a.list()

	return append(list.actions(),
		keys.Action[*model]{
			ID:   actionArchiveBack,
			Keys: keys.FixedBack,
			Nav:  func(m *model, _ string) screen.Request { return screen.PickView{} },
		},
	)
}

func (a *archivedSchedules) legend() []keys.LegendRow[*model] {
	return slices.Concat([]keys.LegendRow[*model]{
		{
			Of:   []keys.ID{keys.FilterOpen},
			Desc: filterDescOf,
		},
	}, keys.NavLegend[*model](), []keys.LegendRow[*model]{
		{
			Of:   []keys.ID{actionArchiveBack},
			Desc: func(*model) string { return "back" },
		},
		{
			Of:    []keys.ID{keys.FilterClear},
			Desc:  filterClearDescOf,
			Strip: true,
		},
		{
			Of:    []keys.ID{keys.FilterMatch},
			Desc:  func(*model) string { return "next/prev match" },
			Strip: true,
		},
	})
}
