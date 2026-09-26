package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the documents screen as one table and the legend that names it.
func (d *documentsScreen) actions() screenActionSpec {
	return screenActionSpec{
		table:  d.table(),
		legend: d.legend(),
	}
}

func (d *documentsScreen) table() []screenAction {
	return append(d.list().actions(),
		screenAction{
			id:   actionDocumentsBack,
			keys: func(k config.Keybindings) []string { return k.Documents.Back },
			run: func(h host, _ panelShared, _ string) tea.Cmd {
				return h.enter(screenAgents, screenTarget{agentID: d.agentID})
			},
		},
	)
}

func (d *documentsScreen) legend() []screenLegendRow {
	return slices.Concat(screenNavLegend(), []screenLegendRow{
		{
			of:    []actionID{actionDocumentsBack},
			desc:  func(panelShared) string { return "back" },
			strip: true,
		},
	})
}

// hasDocuments reports whether the hovered agent, or the orchestrator behind a hovered
// workflow, has written anything the documents screen would list.
func (a *agentsScreen) hasDocuments(s panelShared) bool {
	if !a.isBrowsing(s) {
		return false
	}

	row, ok := a.hoveredRow(s)
	if !ok {
		return false
	}

	item, held := row.paneItem()

	return held && item.Documents > 0
}
