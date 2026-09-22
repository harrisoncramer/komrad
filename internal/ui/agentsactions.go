package ui

import (
	"slices"

	"weave/config"
	"weave/internal/ui/panel"

	tea "github.com/charmbracelet/bubbletea"
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
			id:   actionArchiveSelected,
			keys: func(k config.Keybindings) []string { return k.Agents.Archive },
			when: list.isSelecting,
			run:  a.archiveChosen,
		},
		screenAction{
			id:   actionAgentShowHidden,
			keys: func(k config.Keybindings) []string { return k.Agents.ShowHidden },
			run:  a.toggleHidden,
		},
		screenAction{
			id:   actionAgentsBack,
			keys: func(k config.Keybindings) []string { return k.Agents.Back },
			when: list.isBrowsing,
			run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.hide() },
		},
		screenAction{
			id:   actionWorkflowNew,
			keys: func(k config.Keybindings) []string { return k.Workflows.New },
			when: list.isBrowsing,
			run:  func(h host, _ panelShared, _ string) tea.Cmd { return newWorkflowForm(h) },
		},
		screenAction{
			id:   actionAgentSearch,
			keys: func(k config.Keybindings) []string { return k.Agents.Search },
			when: list.isBrowsing,
			run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.enter(screenSearch, screenTarget{}) },
		},
		screenAction{
			id:   actionSchedulesOpen,
			keys: func(k config.Keybindings) []string { return k.Schedules.Open },
			when: list.isBrowsing,
			run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.enter(screenSchedules, screenTarget{}) },
		},
		screenAction{
			id:   actionArchiveOpen,
			keys: func(k config.Keybindings) []string { return k.Archive.Open },
			when: list.isBrowsing,
			run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.enter(screenArchive, screenTarget{}) },
		},
		screenAction{
			id:   actionWorktreesOpen,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Open },
			when: list.isBrowsing,
			run: func(h host, s panelShared, _ string) tea.Cmd {
				return h.enter(screenWorktrees, screenTarget{agentID: a.hoveredAgentID(s)})
			},
		},
		screenAction{
			id:   actionMarkJump,
			keys: func(k config.Keybindings) []string { return k.Marks.Jump },
			when: list.isBrowsing,
			run:  func(host, panelShared, string) tea.Cmd { return a.beginMark(markPendingJump) },
		},
		screenAction{
			id:   actionMarkSet,
			keys: func(k config.Keybindings) []string { return k.Marks.Set },
			when: list.isBrowsing,
			run:  func(host, panelShared, string) tea.Cmd { return a.beginMark(markPendingSet) },
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
			run: func(h host, s panelShared, _ string) tea.Cmd {
				if workflow, ok := a.hoveredWorkflow(s); ok {
					return openNewAgent(h, &workflow)
				}

				return openNewAgent(h, a.enclosingWorkflow(s))
			},
		},
		{
			id:   actionWorkflowAdopt,
			keys: func(k config.Keybindings) []string { return k.Workflows.Adopt },
			when: a.isHoveringWorkflow,
			run:  a.onHoveredWorkflow(func(h host, workflow panel.Workflow) tea.Cmd { return openAdopt(h, &workflow) }),
		},
		{
			id:   actionWorkflowRename,
			keys: func(k config.Keybindings) []string { return k.Workflows.Rename },
			when: a.isHoveringWorkflow,
			run:  a.onHoveredWorkflow(renameWorkflowForm),
		},
		{
			id:   actionWorkflowEdit,
			keys: func(k config.Keybindings) []string { return k.Workflows.Edit },
			when: a.isHoveringWorkflow,
			run:  a.onHoveredWorkflowIn(editWorkflowForm),
		},
		{
			id:   actionWorkflowArchive,
			keys: func(k config.Keybindings) []string { return k.Workflows.Archive },
			when: a.isHoveringWorkflow,
			run:  a.onHoveredWorkflow(archiveWorkflowConfirm),
		},
		{
			id:   actionAgentOpen,
			keys: func(k config.Keybindings) []string { return k.Agents.Open },
			when: list.isBrowsing,
			run:  func(h host, s panelShared, _ string) tea.Cmd { return a.openHovered(h, s) },
		},
		{
			id:   actionAgentPause,
			keys: func(k config.Keybindings) []string { return k.Agents.Pause },
			when: a.isHoveringAgent,
			run:  a.onHoveredAgent(func(h host, item panel.AgentRow) tea.Cmd { return pauseAgent(h, item.Agent) }),
		},
		{
			id:   actionAgentReleaseSilently,
			keys: func(k config.Keybindings) []string { return k.Agents.ReleaseSilently },
			when: a.isHeldAgent,
			run:  a.onHoveredAgent(func(h host, item panel.AgentRow) tea.Cmd { return releaseInSilenceConfirm(h, item.Agent) }),
		},
		{
			id:   actionAgentHide,
			keys: func(k config.Keybindings) []string { return k.Agents.Hide },
			when: a.isHoveringAgent,
			run:  a.onHoveredAgent(func(h host, item panel.AgentRow) tea.Cmd { return hideAgent(h, item.Agent) }),
		},
		{
			id:   actionGitMenu,
			keys: func(k config.Keybindings) []string { return k.Git.Open },
			when: a.isHoveringAgent,
			run:  a.onHoveredAgentIn(openGitMenu),
		},
		{
			id:   actionAgentReview,
			keys: func(k config.Keybindings) []string { return k.Agents.Review },
			when: a.isHoveringAgent,
			run: a.onHoveredAgent(func(h host, item panel.AgentRow) tea.Cmd {
				return h.enter(screenReview, screenTarget{agentID: item.Agent.ID})
			}),
		},
		{
			id:   actionAgentChild,
			keys: func(k config.Keybindings) []string { return k.Agents.Child },
			when: a.isHoveringAgent,
			run:  a.onHoveredAgent(func(h host, item panel.AgentRow) tea.Cmd { return openNewChild(h, item.Agent) }),
		},
		{
			id:   actionAgentRename,
			keys: func(k config.Keybindings) []string { return k.Agents.Rename },
			when: a.isHoveringAgent,
			run:  a.onHoveredAgent(func(h host, item panel.AgentRow) tea.Cmd { return openRename(h, item.Agent) }),
		},
		{
			id:   actionAgentRetry,
			keys: func(k config.Keybindings) []string { return k.Agents.Retry },
			when: a.isRetryableAgent,
			run:  a.onHoveredAgent(func(h host, item panel.AgentRow) tea.Cmd { return openRetryForm(h, item.Agent) }),
		},
		{
			id:   actionAgentArchive,
			keys: func(k config.Keybindings) []string { return k.Agents.Archive },
			when: a.isHoveringAgent,
			run: a.onHoveredAgentIn(func(h host, s panelShared, item panel.AgentRow) tea.Cmd {
				return confirmArchiveAgents(h, s.listing.liveAgents(), []panel.Agent{item.Agent})
			}),
		},
	}
}

func (a *agentsScreen) isBrowsing(s panelShared) bool {
	return a.list().isBrowsing(s)
}

func (a *agentsScreen) isHoveringWorkflow(s panelShared) bool {
	_, ok := a.hoveredWorkflow(s)

	return a.isBrowsing(s) && ok
}

func (a *agentsScreen) isHoveringAgent(s panelShared) bool {
	_, ok := a.hoveredWorkflow(s)

	return a.isBrowsing(s) && !ok
}

// isHeldAgent reports whether the row under the cursor is an agent somebody has paused.
func (a *agentsScreen) isHeldAgent(s panelShared) bool {
	item, ok := a.selectedAgent(s)

	return a.isHoveringAgent(s) && ok && item.Agent.HeldAt != nil
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
	return append(a.selectionLegend(), a.browsingLegend()...)
}

// selectionLegend is what the screen names while a selection stands.
func (a *agentsScreen) selectionLegend() []screenLegendRow {
	list := a.list()

	return append(list.selectionLegend(),
		screenLegendRow{
			of:   []actionID{actionArchiveSelected},
			desc: func(panelShared) string { return "archive all" },
			show: list.isSelecting,
		},
		screenLegendRow{
			of:   []actionID{actionAgentShowHidden},
			desc: a.showHiddenDescOf,
			show: list.isSelecting,
		},
	)
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
			of:   []actionID{actionGitMenu},
			desc: func(panelShared) string { return "git actions" },
			show: a.isHoveringAgent,
		},
		// The review key is drawn for every agent, whether or not it answers for the one hovered.
		{
			of:   []actionID{actionAgentReview},
			desc: func(panelShared) string { return "review diff" },
			show: a.isHoveringAgent,
		},
		{
			of:   []actionID{actionAgentPause},
			desc: a.pauseDesc,
			show: a.isHoveringAgent,
		},
		{
			of:   []actionID{actionAgentReleaseSilently},
			desc: func(panelShared) string { return "unpause in silence" },
			show: a.isHeldAgent,
		},
		{
			of:   []actionID{actionMarkSet},
			desc: func(panelShared) string { return "set mark" },
			show: a.isBrowsing,
		},
		{
			of:   []actionID{actionMarkJump},
			desc: func(panelShared) string { return "jump to mark" },
			show: a.isBrowsing,
		},
		{
			of:   []actionID{actionAgentHide, actionAgentShowHidden},
			desc: a.hideDesc,
			show: a.isHoveringAgent,
		},
		{
			of:   []actionID{actionAgentShowHidden},
			desc: a.showHiddenDescOf,
			show: a.isHoveringWorkflow,
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
			of:   []actionID{actionWorkflowArchive, actionArchiveOpen},
			desc: func(panelShared) string { return "archive/archived agents" },
			show: a.isHoveringWorkflow,
		},
		{
			of:   []actionID{actionAgentArchive, actionArchiveOpen},
			desc: func(panelShared) string { return "archive/archived agents" },
			show: a.isHoveringAgent,
		},
		{
			of:   []actionID{actionAgentSearch},
			desc: func(panelShared) string { return "search" },
			show: a.isBrowsing,
		},
		{
			of:   []actionID{actionSchedulesOpen},
			desc: func(panelShared) string { return "schedules" },
			show: a.isBrowsing,
		},
		{
			of:   []actionID{actionWorktreesOpen},
			desc: func(panelShared) string { return "worktrees" },
			show: a.isBrowsing,
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

func (a *agentsScreen) showHiddenDescOf(panelShared) string {
	return a.showHiddenDesc()
}

func (a *agentsScreen) showHiddenDesc() string {
	if a.showHidden {
		return "put hidden away"
	}

	return "show hidden"
}

// pauseDesc names the pause key for what pressing it would do to the row under the cursor.
func (a *agentsScreen) pauseDesc(s panelShared) string {
	if item, ok := a.selectedAgent(s); ok && item.Agent.HeldAt != nil {
		return "unpause it"
	}

	return "pause it"
}

func (a *agentsScreen) hideDesc(s panelShared) string {
	hide := "hide"
	if item, ok := a.selectedAgent(s); ok && item.Agent.Hidden {
		hide = "unhide"
	}

	return hide + "/" + a.showHiddenDesc()
}

func concatKeys(lists ...[]string) []string {
	var out []string
	for _, list := range lists {
		out = append(out, list...)
	}

	return out
}
