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
			nav: func(m *model, _ string) tea.Cmd {
				a.jumpToTriage(m)

				return nil
			},
		},
		screenAction{
			id:   actionAgentsBack,
			keys: fixedBack,
			when: list.isBrowsing,
			nav:  func(m *model, _ string) tea.Cmd { return m.pickView() },
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
			direct: directly(notInMenu, func(m *model, _ string) tea.Cmd { return a.openRename(m) }),
		},
		{
			id:     actionAgentOpen,
			keys:   fixedOpen,
			when:   list.isBrowsing,
			direct: directly("enter is how a person reaches an agent, and the resume is incidental to that", func(m *model, _ string) tea.Cmd { return a.openHovered(m) }),
		},
		{
			id:     actionAgentRename,
			keys:   func(k config.Keybindings) []string { return k.Agents.Rename },
			when:   a.isHoveringAgent,
			direct: directly(notInMenu, func(m *model, _ string) tea.Cmd { return a.openRename(m) }),
		},
	}
}

func (a *agentsScreen) isBrowsing(m *model) bool {
	return a.list().isBrowsing(m)
}

// isTriageReachable reports whether the triage key has a zone to jump to.
func (a *agentsScreen) isTriageReachable(m *model) bool {
	return a.triage.isOn() && a.isBrowsing(m)
}

func (a *agentsScreen) isHoveringWorkflow(m *model) bool {
	_, ok := a.hoveredWorkflow(m)

	return a.isBrowsing(m) && ok
}

func (a *agentsScreen) isHoveringAgent(m *model) bool {
	_, ok := a.hoveredWorkflow(m)

	return a.isBrowsing(m) && !ok
}

func (a *agentsScreen) isRetryableAgent(m *model) bool {
	item, ok := a.selectedAgent(m)

	return a.isHoveringAgent(m) && ok && item.Agent.State == panel.StateFailedToCreate
}

func (a *agentsScreen) legend() []screenLegendRow {
	return append(a.list().selectionLegend(), a.browsingLegend()...)
}

// browsingLegend is the ordinary legend, in reading order.
func (a *agentsScreen) browsingLegend() []screenLegendRow {
	return slices.Concat([]screenLegendRow{
		{
			of:    []actionID{actionAgentOpen},
			desc:  func(*model) string { return "open" },
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
			desc: func(*model) string { return "triage" },
			show: a.isTriageReachable,
		},
		{
			of:   []actionID{actionWorkflowRename},
			desc: func(*model) string { return "rename" },
		},
		{
			of:   []actionID{actionAgentRename},
			desc: func(*model) string { return "rename" },
			show: a.isHoveringAgent,
		},
		{
			of:   []actionID{actionFilterOpen},
			desc: filterDescOf,
			show: a.isBrowsing,
		},
		{
			of:   []actionID{actionFilterMatch},
			desc: func(*model) string { return "next/prev match" },
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
