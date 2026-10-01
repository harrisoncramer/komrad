package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// actions is the agents list as one table and the legend that names it.
func (a *agentsScreen) actions() screenActionSpec {
	return screenActionSpec{
		table:  a.table(),
		legend: a.legend(),
	}
}

func (a *agentsScreen) table() []screenAction {
	return append(a.modeActions(), a.rowActions()...)
}

// modeActions are the keys that answer whatever the cursor is on.
func (a *agentsScreen) modeActions() []screenAction {
	list := a.list()

	return append(list.actions(),
		screenAction{
			id:   actionAgentTriage,
			keys: func(k config.Keybindings) []string { return k.Agents.Triage },
			when: a.isTriageReachable,
			nav: func(h navHost, s panelShared, _ string) tea.Cmd {
				a.jumpToTriage(h, s)

				return nil
			},
		},
		screenAction{
			id:   actionAgentsBack,
			keys: func(k config.Keybindings) []string { return k.Agents.Back },
			when: list.isBrowsing,
			nav:  func(h navHost, _ panelShared, _ string) tea.Cmd { return h.pickView() },
		},
	)
}

// rowActions are the keys that act on the row the cursor is on, a workflow or an agent and never
// both.
func (a *agentsScreen) rowActions() []screenAction {
	list := a.list()

	return []screenAction{
		{
			id:     actionWorkflowRename,
			keys:   func(k config.Keybindings) []string { return k.Workflows.Rename },
			when:   a.isHoveringWorkflow,
			direct: directly(notInMenu, func(_ host, s panelShared, _ string) tea.Cmd { return a.openRename(s) }),
		},
		{
			id:     actionAgentOpen,
			keys:   func(k config.Keybindings) []string { return k.Agents.Open },
			when:   list.isBrowsing,
			direct: directly("enter is how a person reaches an agent, and the resume is incidental to that", func(h host, s panelShared, _ string) tea.Cmd { return a.openHovered(h, s) }),
		},
		{
			id:     actionAgentRename,
			keys:   func(k config.Keybindings) []string { return k.Agents.Rename },
			when:   a.isHoveringAgent,
			direct: directly(notInMenu, func(_ host, s panelShared, _ string) tea.Cmd { return a.openRename(s) }),
		},
	}
}

func (a *agentsScreen) isBrowsing(s panelShared) bool {
	return a.list().isBrowsing(s)
}

// isTriageReachable reports whether the triage key has a zone to jump to.
func (a *agentsScreen) isTriageReachable(s panelShared) bool {
	return a.triage.isOn() && a.isBrowsing(s)
}

func (a *agentsScreen) isHoveringWorkflow(s panelShared) bool {
	_, ok := a.hoveredWorkflow(s)

	return a.isBrowsing(s) && ok
}

func (a *agentsScreen) isHoveringAgent(s panelShared) bool {
	_, ok := a.hoveredWorkflow(s)

	return a.isBrowsing(s) && !ok
}

func (a *agentsScreen) isRetryableAgent(s panelShared) bool {
	item, ok := a.selectedAgent(s)

	return a.isHoveringAgent(s) && ok && item.Agent.State == panel.StateFailedToCreate
}

func (a *agentsScreen) legend() []screenLegendRow {
	return append(a.list().selectionLegend(), a.browsingLegend()...)
}

// browsingLegend is the ordinary legend, in reading order.
func (a *agentsScreen) browsingLegend() []screenLegendRow {
	return slices.Concat([]screenLegendRow{
		{
			of:    []actionID{actionAgentOpen},
			desc:  func(panelShared) string { return "open" },
			show:  a.isBrowsing,
			strip: true,
		},
		{
			of:   []actionID{actionSelectToggle},
			desc: a.list().toggleDesc,
			show: a.isBrowsing,
		},
		{
			of:   []actionID{actionAgentTriage},
			desc: func(panelShared) string { return "triage" },
			show: a.isTriageReachable,
		},
		{
			of:   []actionID{actionWorkflowRename},
			desc: func(panelShared) string { return "rename" },
		},
		{
			of:   []actionID{actionAgentRename},
			desc: func(panelShared) string { return "rename" },
			show: a.isHoveringAgent,
		},
		{
			of:   []actionID{actionFilterOpen},
			desc: filterDescOf,
			show: a.isBrowsing,
		},
		{
			of:   []actionID{actionFilterMatch},
			desc: func(panelShared) string { return "next/prev match" },
		},
		{
			of:   []actionID{actionFilterClear},
			desc: filterClearDescOf,
		},
	}, screenNavLegend())
}

func (a *agentsScreen) showHiddenDesc() string {
	if a.showHidden {
		return "put hidden away"
	}

	return "show hidden"
}
