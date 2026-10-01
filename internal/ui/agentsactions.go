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
				a.toggleTriage(h, s)

				return nil
			},
		},
		screenAction{
			id:   actionAgentTriageBack,
			keys: func(k config.Keybindings) []string { return k.Agents.Back },
			when: func(s panelShared) bool { return a.triage.focused && list.isBrowsing(s) },
			nav: func(h navHost, s panelShared, _ string) tea.Cmd {
				a.toggleTriage(h, s)

				return nil
			},
		},
		screenAction{
			id:   actionAgentsBack,
			keys: func(k config.Keybindings) []string { return k.Agents.Back },
			when: list.isBrowsing,
			nav:  func(h navHost, _ panelShared, _ string) tea.Cmd { return h.pickView() },
		},
		screenAction{
			id:     actionWorkflowNew,
			keys:   func(k config.Keybindings) []string { return k.Workflows.New },
			when:   list.isBrowsing,
			direct: directly(notInMenu, func(h host, s panelShared, _ string) tea.Cmd { return newWorkflowForm(h, s) }),
		},
	)
}

// rowActions are the keys that act on the row the cursor is on, a workflow or an agent and never
// both.
func (a *agentsScreen) rowActions() []screenAction {
	list := a.list()

	return []screenAction{
		{
			id:   actionAgentNew,
			keys: func(k config.Keybindings) []string { return k.Agents.New },
			when: list.isBrowsing,
			direct: directly(notInMenu, func(h host, s panelShared, _ string) tea.Cmd {
				if workflow, ok := a.hoveredWorkflow(s); ok {
					return openNewAgent(h, &workflow)
				}

				return openNewAgent(h, a.enclosingWorkflow(s))
			}),
		},
		{
			id:     actionWorkflowAdopt,
			keys:   func(k config.Keybindings) []string { return k.Workflows.Adopt },
			when:   a.isHoveringWorkflow,
			direct: directly(notInMenu, a.onHoveredWorkflow(func(h host, workflow panel.Workflow) tea.Cmd { return openAdopt(h, &workflow) })),
		},
		{
			id:     actionWorkflowRename,
			keys:   func(k config.Keybindings) []string { return k.Workflows.Rename },
			when:   a.isHoveringWorkflow,
			direct: directly(notInMenu, func(_ host, s panelShared, _ string) tea.Cmd { return a.openRename(s) }),
		},
		{
			id:     actionWorkflowEdit,
			keys:   func(k config.Keybindings) []string { return k.Workflows.Edit },
			when:   func(s panelShared) bool { return a.editing == nil && a.isHoveringWorkflow(s) },
			direct: directly(notInMenu, a.onHoveredWorkflowIn(editWorkflowForm)),
		},
		{
			id:     actionAgentOpen,
			keys:   func(k config.Keybindings) []string { return k.Agents.Open },
			when:   list.isBrowsing,
			direct: directly("enter is how a person reaches an agent, and the resume is incidental to that", func(h host, s panelShared, _ string) tea.Cmd { return a.openHovered(h, s) }),
		},
		{
			id:     actionAgentEdit,
			keys:   func(k config.Keybindings) []string { return k.Agents.Edit },
			when:   a.isEditable,
			direct: directly(notInMenu, func(h host, s panelShared, _ string) tea.Cmd { return a.pressEdit(h, s) }),
		},
		{
			id:     actionAgentChild,
			keys:   func(k config.Keybindings) []string { return k.Agents.Child },
			when:   a.isHoveringAgent,
			direct: directly(notInMenu, a.onHoveredAgent(func(h host, item panel.AgentRow) tea.Cmd { return openNewChild(h, item.Agent) })),
		},
		{
			id:     actionAgentRename,
			keys:   func(k config.Keybindings) []string { return k.Agents.Rename },
			when:   a.isHoveringAgent,
			direct: directly(notInMenu, func(_ host, s panelShared, _ string) tea.Cmd { return a.openRename(s) }),
		},
		{
			id:     actionAgentRetry,
			keys:   func(k config.Keybindings) []string { return k.Agents.Retry },
			when:   a.isRetryableAgent,
			direct: directly(notInMenu, a.onHoveredAgent(func(h host, item panel.AgentRow) tea.Cmd { return openRetryForm(h, item.Agent) })),
		},
	}
}

func (a *agentsScreen) isBrowsing(s panelShared) bool {
	return a.list().isBrowsing(s)
}

// isTriageReachable reports whether the triage key has a zone to move into.
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

// onHoveredWorkflow hands an arm the workflow under the cursor.
func (a *agentsScreen) onHoveredWorkflow(fn func(host, panel.Workflow) tea.Cmd) func(host, panelShared, string) tea.Cmd {
	return a.onHoveredWorkflowIn(func(h host, _ panelShared, workflow panel.Workflow) tea.Cmd { return fn(h, workflow) })
}

// onHoveredWorkflowIn hands an arm the workflow under the cursor and the listing it stands in.
func (a *agentsScreen) onHoveredWorkflowIn(fn func(host, panelShared, panel.Workflow) tea.Cmd) func(host, panelShared, string) tea.Cmd {
	return func(h host, s panelShared, _ string) tea.Cmd {
		workflow, ok := a.hoveredWorkflow(s)
		if !ok {
			return nil
		}

		return fn(h, s, workflow)
	}
}

// onHoveredAgent hands an arm the agent under the cursor.
func (a *agentsScreen) onHoveredAgent(fn func(host, panel.AgentRow) tea.Cmd) func(host, panelShared, string) tea.Cmd {
	return a.onHoveredAgentIn(func(h host, _ panelShared, item panel.AgentRow) tea.Cmd { return fn(h, item) })
}

// onHoveredAgentIn hands an arm the agent under the cursor and the listing it stands in.
func (a *agentsScreen) onHoveredAgentIn(fn func(host, panelShared, panel.AgentRow) tea.Cmd) func(host, panelShared, string) tea.Cmd {
	return func(h host, s panelShared, _ string) tea.Cmd {
		item, ok := a.selectedAgent(s)
		if !ok {
			return nil
		}

		return fn(h, s, item)
	}
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
			of:   []actionID{actionWorkflowAdopt},
			desc: func(panelShared) string { return "adopt a branch or pr" },
		},
		{
			of:   []actionID{actionSelectToggle},
			desc: a.list().toggleDesc,
			show: a.isBrowsing,
		},
		{
			of:   []actionID{actionAgentEdit},
			desc: a.editDesc,
			show: a.isEditable,
		},
		{
			of:   []actionID{actionAgentTriage},
			desc: a.triageDesc,
			show: a.isTriageReachable,
		},
		{
			of:   []actionID{actionAgentNew, actionWorkflowNew},
			desc: func(panelShared) string { return "new agent/workflow" },
			show: a.isBrowsing,
		},
		{
			of:   []actionID{actionAgentRetry},
			desc: func(panelShared) string { return "retry launch" },
			show: a.isRetryableAgent,
		},
		{
			of:   []actionID{actionAgentChild},
			desc: func(panelShared) string { return "attach child" },
			show: a.isHoveringAgent,
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
			of:   []actionID{actionWorkflowEdit},
			desc: func(panelShared) string { return "edit" },
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

func (a *agentsScreen) triageDesc(panelShared) string {
	if a.triage.focused {
		return "leave triage"
	}

	return "triage"
}

func (a *agentsScreen) showHiddenDesc() string {
	if a.showHidden {
		return "put hidden away"
	}

	return "show hidden"
}
