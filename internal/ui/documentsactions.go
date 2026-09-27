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
			id:   actionDocumentsEdit,
			keys: func(k config.Keybindings) []string { return k.Documents.Edit },
			run: func(h host, s panelShared, _ string) tea.Cmd {
				return d.pressEdit(h, s)
			},
		},
		screenAction{
			id:   actionDocumentsNew,
			keys: func(k config.Keybindings) []string { return k.Documents.New },
			run: func(h host, s panelShared, _ string) tea.Cmd {
				return d.newDocumentForm(h, s)
			},
		},
		screenAction{
			id:   actionDocumentsOlder,
			keys: func(k config.Keybindings) []string { return k.Documents.Older },
			run: func(h host, s panelShared, _ string) tea.Cmd {
				return d.stepOlder(h, s)
			},
		},
		screenAction{
			id:   actionDocumentsNewer,
			keys: func(k config.Keybindings) []string { return k.Documents.Newer },
			run: func(h host, s panelShared, _ string) tea.Cmd {
				return d.stepNewer(h, s)
			},
		},
		screenAction{
			id:   actionDocumentsPan,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Documents.PanLeft, k.Documents.PanRight) },
			when: d.isSteppingHovered,
			run:  d.panBy,
		},
		screenAction{
			id:   actionDocumentsPanReset,
			keys: func(k config.Keybindings) []string { return k.Documents.PanReset },
			when: d.isSteppingHovered,
			run: func(host, panelShared, string) tea.Cmd {
				d.history.pan = 0

				return nil
			},
		},
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
			of:   []actionID{actionFilterOpen},
			desc: filterDescOf,
		},
		{
			of:   []actionID{actionDocumentsEdit},
			desc: func(panelShared) string { return d.editDesc() },
		},
		{
			of:   []actionID{actionDocumentsNew},
			desc: func(panelShared) string { return "new document" },
		},
		{
			of:   []actionID{actionDocumentsOlder},
			desc: func(panelShared) string { return "an earlier version" },
		},
		{
			of:   []actionID{actionDocumentsNewer},
			desc: func(panelShared) string { return "a later version" },
		},
		{
			of:   []actionID{actionDocumentsPan, actionDocumentsPanReset},
			desc: func(panelShared) string { return "pan sideways" },
			show: d.isSteppingHovered,
			label: func(k config.Keybindings) string {
				return headLabel(k.Documents.PanLeft) + "/" + headLabel(k.Documents.PanRight) + "/" + keyLabel(k.Documents.PanReset)
			},
		},
		{
			of:    []actionID{actionFilterClear},
			desc:  filterClearDescOf,
			strip: true,
		},
		{
			of:    []actionID{actionFilterMatch},
			desc:  func(panelShared) string { return "next/prev match" },
			strip: true,
		},
		{
			of:    []actionID{actionDocumentsBack},
			desc:  func(panelShared) string { return "back" },
			strip: true,
		},
	})
}

func (d *documentsScreen) panBy(_ host, s panelShared, pressed string) tea.Cmd {
	step := panStep(s)
	if isBound(pressed, s.keys.Documents.PanLeft) {
		step = -step
	}

	d.history.pan = max(0, d.history.pan+step)

	return nil
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
