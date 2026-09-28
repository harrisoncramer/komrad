package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the logs screen as one table and the legend that names it.
func (l *logsScreen) actions() screenActionSpec {
	return screenActionSpec{
		table:  l.table(),
		legend: l.legend(),
	}
}

func (l *logsScreen) table() []screenAction {
	return append(l.list().actions(),
		screenAction{
			id:   actionLogsBack,
			keys: func(k config.Keybindings) []string { return k.Logs.Back },
			run: func(h host, _ panelShared, _ string) tea.Cmd {
				return h.enter(l.from, screenTarget{agentID: l.agentID})
			},
		},
	)
}

func (l *logsScreen) legend() []screenLegendRow {
	return slices.Concat(screenNavLegend(), []screenLegendRow{
		{
			of:    []actionID{actionLogsBack},
			desc:  func(panelShared) string { return "back" },
			strip: true,
		},
	})
}

func (l *logsScreen) help(s panelShared) keyMap {
	return l.actions().help(s)
}
