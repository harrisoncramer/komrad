package ui

import (
	"fmt"

	"weave/config"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
)

// agentsActions is the agents list as one table, walked in precedence order by the dispatch and
// named in reading order by a legend of the same ids.
func agentsActions() actionSpec {
	return actionSpec{
		table:  agentsTable(),
		legend: agentsLegend(),
	}
}

func agentsTable() []action {
	return append(agentsModeActions(), agentsRowActions()...)
}

// agentsModeActions are the keys that answer whatever the cursor is on, ordered ahead of the row
// keys.
func agentsModeActions() []action {
	return []action{
		{
			id:   actionJump,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Top, k.Nav.Bottom) },
			run:  model.jumpAgents,
		},
		{
			id:   actionSelectClear,
			keys: func(k config.Keybindings) []string { return k.Selection.Clear },
			when: func(m model) bool { return m.tree.selected.isActive() },
			run:  model.clearAgentSelection,
		},
		{
			id:   actionSelectToggle,
			keys: func(k config.Keybindings) []string { return k.Selection.Toggle },
			run:  model.toggleAgentSelection,
		},
		{
			id:   actionSelectRange,
			keys: func(k config.Keybindings) []string { return k.Selection.Range },
			run:  model.extendAgentSelection,
		},
		{
			id:   actionArchiveSelected,
			keys: func(k config.Keybindings) []string { return k.Agents.Archive },
			when: func(m model) bool { return m.tree.selected.isActive() },
			run:  model.archiveChosenAgents,
		},
		{
			id:   actionAgentShowHidden,
			keys: func(k config.Keybindings) []string { return k.Agents.ShowHidden },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.toggleHiddenAgents() },
		},
		{
			id:   actionMove,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Nav.Up, k.Nav.Down) },
			run:  model.moveAgentCursor,
		},
		{
			id:   actionQuit,
			keys: func(k config.Keybindings) []string { return k.Agents.Quit },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m, m.hide() },
		},
		{
			id:   actionFilterOpen,
			keys: func(k config.Keybindings) []string { return k.Filter.Open },
			when: func(m model) bool { return !m.tree.selected.isActive() },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openFilter() },
		},
		{
			id:   actionFilterMatch,
			keys: func(k config.Keybindings) []string { return concatKeys(k.Filter.NextMatch, k.Filter.PrevMatch) },
			when: func(m model) bool { return !m.tree.selected.isActive() && m.filter.isStanding() },
			run:  model.jumpAgentMatch,
		},
		{
			id:   actionFilterClear,
			keys: func(k config.Keybindings) []string { return k.Agents.Back },
			when: func(m model) bool { return !m.tree.selected.isActive() && m.filter.isStanding() },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.clearSearch() },
		},
		{
			id:   actionAgentsBack,
			keys: func(k config.Keybindings) []string { return k.Agents.Back },
			when: func(m model) bool { return !m.tree.selected.isActive() },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m, m.hide() },
		},
		{
			id:   actionWorkflowNew,
			keys: func(k config.Keybindings) []string { return k.Workflows.New },
			when: func(m model) bool { return !m.tree.selected.isActive() },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.newWorkflowForm() },
		},
		{
			id:   actionAgentSearch,
			keys: func(k config.Keybindings) []string { return k.Agents.Search },
			when: func(m model) bool { return !m.tree.selected.isActive() },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.enterSearch() },
		},
		{
			id:   actionSchedulesOpen,
			keys: func(k config.Keybindings) []string { return k.Schedules.Open },
			when: func(m model) bool { return !m.tree.selected.isActive() },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.enterSchedules() },
		},
		{
			id:   actionArchiveOpen,
			keys: func(k config.Keybindings) []string { return k.Archive.Open },
			when: func(m model) bool { return !m.tree.selected.isActive() },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.openArchive() },
		},
		{
			id:   actionMarkJump,
			keys: func(k config.Keybindings) []string { return k.Marks.Jump },
			when: func(m model) bool { return !m.tree.selected.isActive() },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.beginMark(markPendingJump) },
		},
		{
			id:   actionMarkSet,
			keys: func(k config.Keybindings) []string { return k.Marks.Set },
			when: func(m model) bool { return !m.tree.selected.isActive() },
			run:  func(m model, _ string) (tea.Model, tea.Cmd) { return m.beginMark(markPendingSet) },
		},
	}
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
			id:   actionAgentToggleViewed,
			keys: func(k config.Keybindings) []string { return k.Agents.ToggleViewed },
			when: isHoveringAgent,
			run:  onHoveredAgent(model.toggleAgentViewed),
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
			id:   actionAgentMail,
			keys: func(k config.Keybindings) []string { return k.Agents.Mail },
			when: isHoveringAgent,
			run:  onHoveredAgent(func(m model, item agentItem) (tea.Model, tea.Cmd) { return m.enterMail(item.agent) }),
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
				return m.archiveAgentsConfirm([]agentstore.Agent{item.agent})
			}),
		},
	}
}

func isBrowsing(m model) bool {
	return !m.tree.selected.isActive()
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

	return isHoveringAgent(m) && ok && item.agent.State == agentstore.StateFailedToCreate
}

// onHoveredWorkflow hands an arm the workflow under the cursor, which its gate has already said
// is there.
func onHoveredWorkflow(fn func(model, workflowstore.Workflow) (tea.Model, tea.Cmd)) func(model, string) (tea.Model, tea.Cmd) {
	return func(m model, _ string) (tea.Model, tea.Cmd) {
		workflow, ok := m.hoveredWorkflow()
		if !ok {
			return m, nil
		}

		return fn(m, workflow)
	}
}

// onHoveredAgent hands an arm the agent under the cursor, and does nothing on a row holding none.
func onHoveredAgent(fn func(model, agentItem) (tea.Model, tea.Cmd)) func(model, string) (tea.Model, tea.Cmd) {
	return func(m model, _ string) (tea.Model, tea.Cmd) {
		return m.withSelectedAgent(func(item agentItem) (tea.Model, tea.Cmd) { return fn(m, item) })
	}
}

// jumpAgents answers the bottom key, the top key being a doubled gesture the screen has already
// taken before the table is reached.
func (m model) jumpAgents(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Bottom) {
		next, cmd := m.jumpCursor(len(m.agentRows()) - 1)

		return next, cmd
	}

	return m, nil
}

func (m model) moveAgentCursor(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Nav.Down) {
		next, cmd := m.moveCursor(1)

		return next, cmd
	}

	next, cmd := m.moveCursor(-1)

	return next, cmd
}

func (m model) jumpAgentMatch(pressed string) (tea.Model, tea.Cmd) {
	if isBound(pressed, m.keys.Filter.PrevMatch) {
		return m.jumpMatch(-1)
	}

	return m.jumpMatch(1)
}

func (m model) clearAgentSelection(string) (tea.Model, tea.Cmd) {
	m.tree.selected = selection{}

	return m, nil
}

func (m model) toggleAgentSelection(string) (tea.Model, tea.Cmd) {
	m.tree.selected = m.tree.selected.toggle(refsOf(m.agentRows()), m.tree.cursor)

	return m, nil
}

func (m model) extendAgentSelection(string) (tea.Model, tea.Cmd) {
	if !m.tree.selected.isActive() {
		return m.toggleAgentSelection("")
	}

	m.tree.selected = m.tree.selected.extend(refsOf(m.agentRows()), m.tree.cursor)

	return m, nil
}

func (m model) archiveChosenAgents(string) (tea.Model, tea.Cmd) {
	return m.archiveAgentsConfirm(chosenAgents(m.tree.selected, m.agentRows()))
}

func (m model) openAdoptOf(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	return m.openAdopt(&workflow)
}

func agentsLegend() []legendRow {
	return append(agentsSelectionLegend(), agentsBrowsingLegend()...)
}

// agentsSelectionLegend is what the screen names while a selection stands, when the keys that
// act on one agent have stood down.
func agentsSelectionLegend() []legendRow {
	selecting := func(m model) bool { return m.tree.selected.isActive() }

	return []legendRow{
		{
			of:    []actionID{actionSelectToggle},
			desc:  model.selectionToggleDesc,
			show:  selecting,
			strip: true,
		},
		{
			of:   []actionID{actionSelectRange},
			desc: func(model) string { return "select to nearest" },
			show: selecting,
		},
		{
			of:    []actionID{actionSelectClear},
			desc:  model.selectionClearDesc,
			show:  selecting,
			strip: true,
		},
		{
			of:   []actionID{actionArchiveSelected},
			desc: func(model) string { return "archive all" },
			show: selecting,
		},
		{
			of:   []actionID{actionAgentShowHidden},
			desc: model.showHiddenDescOf,
			show: selecting,
		},
	}
}

// agentsBrowsingLegend is the ordinary legend, in reading order rather than in the order the
// dispatch tries the keys.
func agentsBrowsingLegend() []legendRow {
	return []legendRow{
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
			desc: model.selectionToggleDesc,
			show: isBrowsing,
		},
		{
			of:   []actionID{actionGitMenu},
			desc: func(model) string { return "git actions" },
			show: isHoveringAgent,
		},
		// The review key answers for some agents and not others, so it is drawn for any of them and
		// the caveats say which, which keeps the legend one height as the cursor moves.
		{
			of:   []actionID{actionAgentReview},
			desc: func(model) string { return "review diff" },
			show: isHoveringAgent,
		},
		{
			of:   []actionID{actionAgentMail, actionAgentPause},
			desc: model.mailAndPauseDesc,
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
			of:   []actionID{actionAgentToggleViewed},
			desc: func(model) string { return "mark read/unread" },
			show: isHoveringAgent,
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
			desc: func(model) string { return "clear search" },
		},
		{
			of:    []actionID{actionMove},
			desc:  func(model) string { return "move" },
			label: func(k config.Keybindings) string { return keyLabel(k.Nav.Up) + "/" + keyLabel(k.Nav.Down) },
		},
		{
			of:    []actionID{actionJump},
			desc:  func(model) string { return "top/bottom" },
			label: func(k config.Keybindings) string { return doubledLabel(k.Nav.Top) + "/" + keyLabel(k.Nav.Bottom) },
		},
	}
}

func (m model) selectionToggleDesc() string {
	hovered, _ := m.hoveredRow()

	switch {
	case hovered.header:
		return "select folder"
	case m.tree.selected.has(rowKey(hovered)):
		return "deselect"
	}

	return "select"
}

func (m model) selectionClearDesc() string {
	return fmt.Sprintf("clear %d selected", len(m.tree.selected.keys))
}

func (m model) showHiddenDescOf() string {
	return m.showHiddenDesc()
}

// mailAndPauseDesc names the two keys that share a legend row, which is what keeps the expanded
// legend inside a pane that already fits.
func (m model) mailAndPauseDesc() string {
	return "mailbox, " + m.pauseDesc()
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
		return "search: " + m.filter.query
	}

	return "search"
}

func concatKeys(lists ...[]string) []string {
	var out []string
	for _, list := range lists {
		out = append(out, list...)
	}

	return out
}
