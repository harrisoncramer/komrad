package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// actions is the archive screen as one table and the legend that names it.
func (a *archiveScreen) actions() screenActionSpec {
	return screenActionSpec{
		table:  a.table(),
		legend: a.legend(),
	}
}

func (a *archiveScreen) table() []screenAction {
	list := a.scheduleList()

	return append(list.actions(),
		screenAction{
			id:   actionArchiveBack,
			keys: fixedBack,
			nav:  func(m *model, _ string) tea.Cmd { return m.pickView() },
		},
	)
}

func (a *archiveScreen) legend() []screenLegendRow {
	return slices.Concat([]screenLegendRow{
		{
			of:   []actionID{actionFilterOpen},
			desc: filterDescOf,
		},
	}, screenNavLegend(), []screenLegendRow{
		{
			of:   []actionID{actionArchiveBack},
			desc: func(*model) string { return "back" },
		},
		{
			of:    []actionID{actionFilterClear},
			desc:  filterClearDescOf,
			strip: true,
		},
		{
			of:    []actionID{actionFilterMatch},
			desc:  func(*model) string { return "next/prev match" },
			strip: true,
		},
	})
}
