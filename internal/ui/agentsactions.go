package ui

import (
	"weave/config"
	"weave/internal/ui/panel"

	tea "github.com/charmbracelet/bubbletea"
)

// agentsActions is the agents list as one table and the legend that names it.
func agentsActions() actionSpec {
	return actionSpec{
		table:  agentsTable(),
		legend: agentsLegend(),
	}
}

func agentsTable() []action {
	return append(agentsModeActions(), agentsRowActions()...)
}

// agentsModeActions are the keys that answer whatever the cursor is on.
func agentsModeActions() []action {
	list := agentsList()

	return append(list.actions(),
		action{
			id:   actionArchiveSelected,
			keys: func(k config.Keybindings) []string { return k.Agents.Archive },
			when: list.isSelecting,
			run:  model.archiveChosenAgents,
		},
		action{
			id:   actionAgentShowHidden,
			keys: func(k config.Keybindings) []string { return k.Agents.ShowHidden },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.toggleHiddenAgents() },
		},
		action{
			id:   actionAgentsBack,
			keys: func(k config.Keybindings) []string { return k.Agents.Back },
			when: isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m, m.hide() },
		},
		action{
			id:   actionWorkflowNew,
			keys: func(k config.Keybindings) []string { return k.Workflows.New },
			when: isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.newWorkflowForm() },
		},
		action{
			id:   actionAgentSearch,
			keys: func(k config.Keybindings) []string { return k.Agents.Search },
			when: isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.enterSearch() },
		},
		action{
			id:   actionSchedulesOpen,
			keys: func(k config.Keybindings) []string { return k.Schedules.Open },
			when: isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.schedules.open(m, screenTarget{}) },
		},
		action{
			id:   actionArchiveOpen,
			keys: func(k config.Keybindings) []string { return k.Archive.Open },
			when: isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.enterArchive() },
		},
		action{
			id:   actionWorktreesOpen,
			keys: func(k config.Keybindings) []string { return k.Worktrees.Open },
			when: isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openWorktrees(m.hoveredAgentID()) },
		},
		action{
			id:   actionMarkJump,
			keys: func(k config.Keybindings) []string { return k.Marks.Jump },
			when: isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.beginMark(markPendingJump) },
		},
		action{
			id:   actionMarkSet,
			keys: func(k config.Keybindings) []string { return k.Marks.Set },
			when: isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.beginMark(markPendingSet) },
		},
	)
}

// agentsRowActions are the keys that act on the row the cursor is on, a workflow or an agent and
// never both.
func agentsRowActions() []action {
	return []action{
		{
			id:   actionAgentNew,
			keys: func(k config.Keybindings) []string { return k.Agents.New },
			when: isBrowsing,
			run: func(m model, _ string) (tea.Model, tea.Cmd) {
				if workflow, ok := m.hoveredWorkflow(); ok {
					return m.openNewAgent(&workflow)
				}

				return m.openNewAgent(m.enclosingWorkflow())
			},
		},
		{
			id:   actionWorkflowAdopt,
			keys: func(k config.Keybindings) []string { return k.Workflows.Adopt },
			when: isHoveringWorkflow,
			run:  onHoveredWorkflow(model.openAdoptOf),
		},
		{
			id:   actionWorkflowRename,
			keys: func(k config.Keybindings) []string { return k.Workflows.Rename },
			when: isHoveringWorkflow,
			run:  onHoveredWorkflow(model.renameWorkflowForm),
		},
		{
			id:   actionWorkflowEdit,
			keys: func(k config.Keybindings) []string { return k.Workflows.Edit },
			when: isHoveringWorkflow,
			run:  onHoveredWorkflow(model.editWorkflowForm),
		},
		{
			id:   actionWorkflowArchive,
			keys: func(k config.Keybindings) []string { return k.Workflows.Archive },
			when: isHoveringWorkflow,
			run:  onHoveredWorkflow(model.archiveWorkflowConfirm),
		},
		{
			id:   actionAgentOpen,
			keys: func(k config.Keybindings) []string { return k.Agents.Open },
			when: isBrowsing,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openHovered() },
		},
		{
			id:   actionAgentPause,
			keys: func(k config.Keybindings) []string { return k.Agents.Pause },
			when: isHoveringAgent,
			run:  onHoveredAgent(func(m model, item agentItem) (tea.Model, tea.Cmd) { return m.pauseAgent(item.agent) }),
		},
		{
			id:   actionAgentReleaseSilently,
			keys: func(k config.Keybindings) []string { return k.Agents.ReleaseSilently },
			when: isHeldAgent,
			run: onHoveredAgent(func(m model, item agentItem) (tea.Model, tea.Cmd) {
				return m.releaseInSilenceConfirm(item.agent)
			}),
		},
		{
			id:   actionAgentHide,
			keys: func(k config.Keybindings) []string { return k.Agents.Hide },
			when: isHoveringAgent,
			run:  onHoveredAgent(func(m model, item agentItem) (tea.Model, tea.Cmd) { return m.hideAgent(item.agent) }),
		},
		{
			id:   actionGitMenu,
			keys: func(k config.Keybindings) []string { return k.Git.Open },
			when: isHoveringAgent,
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openGitMenu() },
		},
		{
			id:   actionAgentReview,
			keys: func(k config.Keybindings) []string { return k.Agents.Review },
			when: isHoveringAgent,
			run:  onHoveredAgent(func(m model, item agentItem) (tea.Model, tea.Cmd) { return m.enterReview(item.agent) }),
		},
		{
			id:   actionAgentChild,
			keys: func(k config.Keybindings) []string { return k.Agents.Child },
			when: isHoveringAgent,
			run:  onHoveredAgent(func(m model, item agentItem) (tea.Model, tea.Cmd) { return m.openNewChild(item.agent) }),
		},
		{
			id:   actionAgentRename,
			keys: func(k config.Keybindings) []string { return k.Agents.Rename },
			when: isHoveringAgent,
			run:  onHoveredAgent(func(m model, item agentItem) (tea.Model, tea.Cmd) { return m.openRename(item.agent) }),
		},
		{
			id:   actionAgentRetry,
			keys: func(k config.Keybindings) []string { return k.Agents.Retry },
			when: isRetryableAgent,
			run:  onHoveredAgent(func(m model, item agentItem) (tea.Model, tea.Cmd) { return m.openRetryForm(item.agent) }),
		},
		{
			id:   actionAgentArchive,
			keys: func(k config.Keybindings) []string { return k.Agents.Archive },
			when: isHoveringAgent,
			run: onHoveredAgent(func(m model, item agentItem) (tea.Model, tea.Cmd) {
				return m.archiveAgentsConfirm([]panel.Agent{item.agent})
			}),
		},
	}
}

func isBrowsing(m model) bool {
	return agentsList().isBrowsing(m)
}

func isHoveringWorkflow(m model) bool {
	_, ok := m.hoveredWorkflow()

	return isBrowsing(m) && ok
}

func isHoveringAgent(m model) bool {
	_, ok := m.hoveredWorkflow()

	return isBrowsing(m) && !ok
}

// isHeldAgent reports whether the row under the cursor is an agent somebody has paused.
func isHeldAgent(m model) bool {
	item, ok := m.selectedAgent()

	return isHoveringAgent(m) && ok && item.agent.HeldAt != nil
}

func isRetryableAgent(m model) bool {
	item, ok := m.selectedAgent()

	return isHoveringAgent(m) && ok && item.agent.State == panel.StateFailedToCreate
}

// onHoveredWorkflow hands an arm the workflow under the cursor.
func onHoveredWorkflow(fn func(model, panel.Workflow) (tea.Model, tea.Cmd)) func(model, string) (tea.Model, tea.Cmd) {
	return func(m model, _ string) (tea.Model, tea.Cmd) {
		workflow, ok := m.hoveredWorkflow()
		if !ok {
			return m, nil
		}

		return fn(m, workflow)
	}
}

// onHoveredAgent hands an arm the agent under the cursor.
func onHoveredAgent(fn func(model, agentItem) (tea.Model, tea.Cmd)) func(model, string) (tea.Model, tea.Cmd) {
	return func(m model, _ string) (tea.Model, tea.Cmd) {
		return m.withSelectedAgent(func(item agentItem) (tea.Model, tea.Cmd) { return fn(m, item) })
	}
}

func (m model) archiveChosenAgents(string) (tea.Model, tea.Cmd) {
	return m.archiveAgentsConfirm(chosenAgents(m.tree.selected, m.agentRows()))
}

func (m model) openAdoptOf(workflow panel.Workflow) (tea.Model, tea.Cmd) {
	return m.openAdopt(&workflow)
}

func agentsLegend() []legendRow {
	return append(agentsSelectionLegend(), agentsBrowsingLegend()...)
}

// agentsSelectionLegend is what the screen names while a selection stands.
func agentsSelectionLegend() []legendRow {
	list := agentsList()

	return append(list.selectionLegend(),
		legendRow{
			of:   []actionID{actionArchiveSelected},
			desc: func(model) string { return "archive all" },
			show: list.isSelecting,
		},
		legendRow{
			of:   []actionID{actionAgentShowHidden},
			desc: model.showHiddenDescOf,
			show: list.isSelecting,
		},
	)
}

// agentsBrowsingLegend is the ordinary legend, in reading order.
func agentsBrowsingLegend() []legendRow {
	return append([]legendRow{
		{
			of:    []actionID{actionAgentOpen},
			desc:  func(model) string { return "open" },
			show:  isBrowsing,
			strip: true,
		},
		{
			of:   []actionID{actionWorkflowAdopt},
			desc: func(model) string { return "adopt a branch or pr" },
		},
		{
			of:   []actionID{actionSelectToggle},
			desc: agentsList().toggleDesc,
			show: isBrowsing,
		},
		{
			of:   []actionID{actionGitMenu},
			desc: func(model) string { return "git actions" },
			show: isHoveringAgent,
		},
		// The review key is drawn for every agent, whether or not it answers for the one hovered.
		{
			of:   []actionID{actionAgentReview},
			desc: func(model) string { return "review diff" },
			show: isHoveringAgent,
		},
		{
			of:   []actionID{actionAgentPause},
			desc: model.pauseDesc,
			show: isHoveringAgent,
		},
		{
			of:   []actionID{actionAgentReleaseSilently},
			desc: func(model) string { return "unpause in silence" },
			show: isHeldAgent,
		},
		{
			of:   []actionID{actionMarkSet},
			desc: func(model) string { return "set mark" },
			show: isBrowsing,
		},
		{
			of:   []actionID{actionMarkJump},
			desc: func(model) string { return "jump to mark" },
			show: isBrowsing,
		},
		{
			of:   []actionID{actionAgentHide, actionAgentShowHidden},
			desc: model.hideDesc,
			show: isHoveringAgent,
		},
		{
			of:   []actionID{actionAgentShowHidden},
			desc: model.showHiddenDescOf,
			show: isHoveringWorkflow,
		},
		{
			of:   []actionID{actionAgentNew, actionWorkflowNew},
			desc: func(model) string { return "new agent/workflow" },
			show: isBrowsing,
		},
		{
			of:   []actionID{actionAgentRetry},
			desc: func(model) string { return "retry launch" },
			show: isRetryableAgent,
		},
		{
			of:   []actionID{actionAgentChild},
			desc: func(model) string { return "attach child" },
			show: isHoveringAgent,
		},
		{
			of:   []actionID{actionWorkflowRename},
			desc: func(model) string { return "rename" },
		},
		{
			of:   []actionID{actionAgentRename},
			desc: func(model) string { return "rename" },
			show: isHoveringAgent,
		},
		{
			of:   []actionID{actionWorkflowEdit},
			desc: func(model) string { return "edit" },
		},
		{
			of:   []actionID{actionWorkflowArchive, actionArchiveOpen},
			desc: func(model) string { return "archive/archived agents" },
			show: isHoveringWorkflow,
		},
		{
			of:   []actionID{actionAgentArchive, actionArchiveOpen},
			desc: func(model) string { return "archive/archived agents" },
			show: isHoveringAgent,
		},
		{
			of:   []actionID{actionAgentSearch},
			desc: func(model) string { return "search" },
			show: isBrowsing,
		},
		{
			of:   []actionID{actionSchedulesOpen},
			desc: func(model) string { return "schedules" },
			show: isBrowsing,
		},
		{
			of:   []actionID{actionWorktreesOpen},
			desc: func(model) string { return "worktrees" },
			show: isBrowsing,
		},
		{
			of:   []actionID{actionFilterOpen},
			desc: model.filterDesc,
			show: isBrowsing,
		},
		{
			of:   []actionID{actionFilterMatch},
			desc: func(model) string { return "next/prev match" },
		},
		{
			of:   []actionID{actionFilterClear},
			desc: model.filterClearDesc,
		},
	}, navLegend()...)
}

func (m model) showHiddenDescOf() string {
	return m.showHiddenDesc()
}

// pauseDesc names the pause key for what pressing it would do to the row under the cursor.
func (m model) pauseDesc() string {
	if item, ok := m.selectedAgent(); ok && item.agent.HeldAt != nil {
		return "unpause it"
	}

	return "pause it"
}

func (m model) hideDesc() string {
	hide := "hide"
	if item, ok := m.selectedAgent(); ok && item.agent.Hidden {
		hide = "unhide"
	}

	return hide + "/" + m.showHiddenDesc()
}

func (m model) filterDesc() string {
	if m.filter.isStanding() {
		return m.filter.mode.String() + ": " + m.filter.query
	}

	return "search"
}

func (m model) filterClearDesc() string {
	return "clear " + m.filter.mode.String()
}

func concatKeys(lists ...[]string) []string {
	var out []string
	for _, list := range lists {
		out = append(out, list...)
	}

	return out
}

// hoveredAgentID is the agent under the cursor, or the orchestrator behind a hovered workflow.
func (m model) hoveredAgentID() string {
	row, ok := m.hoveredRow()
	if !ok {
		return ""
	}

	item, held := row.paneItem()
	if !held {
		return ""
	}

	return item.agent.ID
}
