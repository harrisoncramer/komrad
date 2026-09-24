package ui

import (
	"fmt"
	"time"

	"weave/config"
	"weave/internal/ui/panel"
	"weave/internal/view"
	agentv1 "weave/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// enter lands on the agent the target names, showing it if it is hidden, or comes back to the
// list as it stands.
func (a *agentsScreen) enter(h host, s panelShared, target screenTarget) tea.Cmd {
	if target.agentID != "" {
		a.revealAgent(s, target.agentID)
	} else {
		a.cursor = clampCursor(a.cursor, len(a.rows(s)))
	}

	h.stageSelection()

	return nil
}

func (a *agentsScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return nil
	}

	return a.list().update(h, s, key, a.actions())
}

func (a *agentsScreen) list() screenList[agentRow] {
	return screenList[agentRow]{
		cursor: func() *rowCursor { return &a.rowCursor },
		rows:   a.rows,
		name:   agentRowName,
		refs:   refsOf,
		header: "folder",
		placed: a.stage,
		moved:  func(h host, _ panelShared) tea.Cmd { return h.waitToStage() },
		quit:   func(k config.Keybindings) []string { return k.Agents.Quit },
		back:   func(k config.Keybindings) []string { return k.Agents.Back },
	}
}

// stage puts the pane of whatever the cursor is standing on beside the list.
func (a *agentsScreen) stage(h host, _ panelShared) {
	h.stageSelection()
}

func (a *agentsScreen) archiveChosen(h host, s panelShared, _ string) tea.Cmd {
	return confirmArchiveAgents(h, s.listing.liveAgents(), chosenAgents(a.selected, a.rows(s)))
}

func (m model) archiveAgents(agents []panel.Agent) (model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.releasePanes(agents)

	return m, m.writeCmd(
		fmt.Sprintf("Could not archive %s", describeAgents(agents)),
		func(a api) error { return a.archiveAgents(panel.IDs(agents)) },
	)
}

func (m model) archiveAndDeleteAgents(agents []panel.Agent) (model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.releasePanes(agents)

	ids := panel.IDs(agents)

	return m, m.writeCmd(
		fmt.Sprintf("Could not archive and delete %s", describeAgents(agents)),
		func(a api) error {
			if err := a.archiveAgents(ids); err != nil {
				return err
			}

			return a.deleteAgents(ids)
		},
	)
}

// openHovered attaches to whatever the cursor is on, which for a workflow is the orchestrator
// standing behind it.
func (a *agentsScreen) openHovered(h host, s panelShared) tea.Cmd {
	if workflow, ok := a.hoveredWorkflow(s); ok {
		return startOrchestrator(h, workflow)
	}

	return a.openAgent(h, s)
}

func (a *agentsScreen) openAgent(h host, s panelShared) tea.Cmd {
	item, ok := a.selectedAgent(s)
	if !ok {
		return nil
	}

	if !item.Alive && !item.Agent.State.DoesPaneDecide() {
		return nil
	}

	if item.Alive {
		if item.Agent.HeldAt == nil {
			h.selectGuest(item.Agent.Pane)
		}

		return nil
	}

	return h.openingWriteCmd(fmt.Sprintf("Could not resume agent %q", item.Agent.Name), func(c api) (*agentv1.Agent, error) {
		return c.resumeAgent(item.Agent.ID)
	})
}

func (a *agentsScreen) toggleHidden(h host, s panelShared, _ string) tea.Cmd {
	selected := a.list().hovered(s)

	a.showHidden = !a.showHidden

	rows := a.rows(s)
	if idx := rowIndexOfKey(rows, selected); idx >= 0 {
		a.cursor = idx
	} else {
		a.cursor = clampCursor(a.cursor, len(rows))
	}
	a.selected = a.selected.pruned(refsOf(a.rows(s)))
	a.stage(h, s)

	return nil
}

func hideAgent(h host, agent panel.Agent) tea.Cmd {
	failure := fmt.Sprintf("Could not hide agent %q", agent.Name)
	if agent.Hidden {
		failure = fmt.Sprintf("Could not unhide agent %q", agent.Name)
	}

	return h.writeCmd(failure, func(a api) error { return a.setHidden(agent, !agent.Hidden) })
}

// unpauseSaid is what an unpaused agent is told.
const unpauseSaid = "I paused you for a moment and have let you go now. " +
	"Look at your worktree as it stands before carrying on. It may have changed while you were paused."

// pauseAgent stops the agent where it stands, or starts one already paused.
func pauseAgent(h host, agent panel.Agent) tea.Cmd {
	if agent.HeldAt != nil {
		return h.releaseCmd(fmt.Sprintf("Could not unpause agent %q", agent.Name), agent, unpauseSaid)
	}

	return h.writeCmd(fmt.Sprintf("Could not pause agent %q", agent.Name), func(a api) error {
		return a.hold(agent)
	})
}

// releaseInSilenceConfirm lets an agent go without typing at it.
func releaseInSilenceConfirm(h host, agent panel.Agent) tea.Cmd {
	form, confirmed := releaseInSilenceForm(agent.Name, formKeys())

	return h.openForm(formState{
		kind:   formAgentRelease,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, m.releaseInSilenceCmd(fmt.Sprintf("Could not unpause agent %q", agent.Name), agent)
		}),
	})
}

func (a *agentsScreen) details(s panelShared) details {
	r, ok := a.hoveredRow(s)
	if !ok {
		return details{}
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, s.listing.agentCount(r.workflow.ID)),
		}
	}

	return details{
		id:      r.item.Agent.ID,
		notices: agentNotices(r),
		fields:  agentDetailFields(s, r.item),
	}
}

// agentDetailFields is the record for an agent as the listing holds it.
func agentDetailFields(s panelShared, item panel.AgentRow) []recordField {
	worktree, read := agentWorktree(s.inspections, item.Agent.Cwd)

	return agentFields(agentFieldsParams{
		agent:        item.Agent,
		schedule:     s.listing.scheduleName(item.Agent),
		parent:       s.listing.parentName(item.Agent),
		workflow:     s.listing.workflowName(item.Agent.WorkflowID),
		standing:     item.PullRequest,
		pullRequests: item.PullRequests,
		worktree:     worktree,
		read:         read,
		now:          time.Now(),
	})
}

// hasPane reports whether an agent has a pane of its own worth putting in the window.
func hasPane(item panel.AgentRow) bool {
	return item.Alive &&
		item.Agent.HeldAt == nil &&
		item.Agent.State != panel.StateSettingUp &&
		!item.Agent.State.IsFailed()
}

// doc is the markdown drawn beside the list for a row with no pane of its own.
func (a *agentsScreen) doc(s panelShared) string {
	r, ok := a.hoveredRow(s)
	item, held := r.paneItem()

	switch {
	case !ok:
		return noAgentsMarkdown(s.keys)
	case r.header && !held:
		return workflowContextMarkdown(r.workflow)
	case item.Agent.State == panel.StateSettingUp:
		return settingUpPlaceholder
	case item.Agent.HeldAt != nil:
		return heldPlaceholder
	case item.Agent.State.IsFailed():
		return failedPlaceholder(s, item.Agent)
	default:
		return dormantPlaceholder
	}
}

// parentName is what the agent a child is attached to is called.
func (l listing) parentName(agent panel.Agent) string {
	if agent.ParentAgentID == nil {
		return ""
	}

	if item, ok := l.liveAgent(*agent.ParentAgentID); ok {
		return item.Agent.Name
	}
	if parent, ok := l.archivedAgent(*agent.ParentAgentID); ok {
		return parent.Name
	}

	return *agent.ParentAgentID
}

func (l listing) scheduleName(agent panel.Agent) string {
	if agent.ScheduleID == nil {
		return ""
	}

	for _, item := range l.schedules {
		if item.schedule.ID == *agent.ScheduleID {
			return item.schedule.Name
		}
	}

	return ""
}

const dormantPlaceholder = "_dormant — not running_\n\nPress open to resume."

const heldPlaceholder = "_paused_\n\nPress unpause to let it carry on, or unpause in silence to let it go without typing at it."

func noAgentsMarkdown(keys config.Keybindings) string {
	key := "w"
	if len(keys.Workflows.New) > 0 {
		key = keys.Workflows.New[0]
	}

	return fmt.Sprintf("# Agents\n\n_nothing here yet_\n\nPress %q to make a workflow and an agent in it.\n", key)
}

const settingUpPlaceholder = "_setting up…_\n\nMaking this agent's worktree. It has no pane yet."

// failureReasonKey is the metadata key a failed create or cleanup records its reason under.
const failureReasonKey = "error"

func failedPlaceholder(s panelShared, agent panel.Agent) string {
	// A create dies at the provisioner or at one of the pre-launch hooks after it, so the run
	// that failed is the newest of either rather than the newest provision run.
	label, moments := "setup", []string{config.TriggerProvision, config.HookPreLaunch}
	// A delete dies either at a pre-teardown hook or at the deprovisioner after it, so the run
	// that failed is the newest of the two.
	if agent.State == panel.StateFailedToCleanup {
		label, moments = "teardown", []string{config.HookPreTeardown, config.TriggerDeprovision}
	}

	if run, ok := latestRunLog(s.runLogsDir, agent.ID, moments); ok {
		return fmt.Sprintf("_%s failed_\n\nPress \"l\" to see run %03d, which is the one that failed.", label, run.Number)
	}

	reason := ""
	for _, entry := range agent.Metadata {
		if entry.Key == failureReasonKey {
			reason = entry.Value
		}
	}
	if reason == "" {
		reason = "no reason was recorded"
	}

	return fmt.Sprintf("_%s failed_\n\n%s", label, reason)
}

func (a *agentsScreen) view(s panelShared) string {
	a.drawn = a.buildRows(s)
	defer func() { a.drawn = nil }()

	return s.draw.columnListView(a.listParams(s))
}

func agentsTabs() tabStrip {
	return tabStrip{
		title:    "↑↓",
		legend:   "sort",
		options:  tabOptionsOf(agentSorts),
		fallback: "created",
		grouped:  "workflow",
	}
}

func (a *agentsScreen) listParams(s panelShared) screenListParams {
	return screenListParams{
		title:    screenTitle(view.GlyphWorkflow, "Agents & Workflows"),
		help:     a.help,
		rows:     len(a.rows(s)),
		snapshot: func(pane listPane) view.Snapshot { return a.snapshot(s, pane) },
	}
}

func (a *agentsScreen) help(s panelShared) keyMap {
	return a.actions().help(s)
}

func (a *agentsScreen) snapshot(s panelShared, pane listPane) view.Snapshot {
	rows := a.rows(s)

	return agentsSnapshot(snapshotParams{
		rows:          rows,
		marks:         s.listing.marks,
		selected:      a.selected,
		scheduled:     s.listing.scheduledWorkflows(),
		focused:       rowKeyAt(rows, a.cursor),
		caret:         pane.caret,
		barBlinkedOut: pane.blinkedOut,
		matcher:       s.chrome.filter.pattern,
		showHidden:    a.showHidden,
		flat:          a.sort.flat,
		pane:          pane.width,
	})
}

// scheduledWorkflows is the workflows at least one schedule launches into.
func (l listing) scheduledWorkflows() map[string]bool {
	scheduled := make(map[string]bool, len(l.schedules))
	for _, item := range l.schedules {
		scheduled[item.schedule.WorkflowID] = true
	}

	return scheduled
}

// startOrchestrator brings the workflow's orchestrator back and puts the keyboard in its pane.
func startOrchestrator(h host, workflow panel.Workflow) tea.Cmd {
	return h.rowWriteCmd(
		fmt.Sprintf("Could not reach the orchestrator of workflow %q", workflow.Name),
		func(a api) (written, error) {
			agent, err := a.startOrchestrator(workflow.ID)
			if err != nil {
				return written{}, err
			}

			// The orchestrator has no row of its own, so the landing goes to the workflow it
			// stands behind.
			return written{
				row:     workflowRowKey(workflow.ID),
				pane:    agent.GetPaneId(),
				harness: agent.GetHarness(),
				focus:   true,
			}, nil
		},
	)
}

var (
	_ detailed = (*agentsScreen)(nil)
	_ filtered = (*agentsScreen)(nil)
	_ tabbed   = (*agentsScreen)(nil)
	_ columned = (*agentsScreen)(nil)
	_ staged   = (*agentsScreen)(nil)
)

func (a *agentsScreen) doesDrawFromListing() bool {
	return true
}

func (a *agentsScreen) takesEveryKey() bool {
	return false
}

func (a *agentsScreen) detailsQuit(keys config.Keybindings) []string {
	return keys.Agents.Quit
}

func (a *agentsScreen) detailsFit(s panelShared) bool {
	return s.draw.listFitsDetails(a.listParams(s))
}

func (a *agentsScreen) detailsRoom(s panelShared) int {
	return s.draw.listRecordRoom(a.listParams(s))
}

func (a *agentsScreen) hoveredKey(s panelShared) string {
	return a.list().hovered(s)
}

func (a *agentsScreen) placeAt(h host, s panelShared, key string) {
	a.list().place(h, s, key)
}

func (a *agentsScreen) filterRows(s panelShared) []filterRow {
	return a.list().filterRows(s)
}

func (a *agentsScreen) tabs() tabStrip {
	return agentsTabs()
}

func (a *agentsScreen) tabsState() *tabsState {
	return &a.sort
}

func (a *agentsScreen) tabsFit(s panelShared) bool {
	return s.draw.listFitsTabs(a.listParams(s))
}

func (a *agentsScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   func(m model) string { return m.agents.doc(m.shared()) },
		holds: func(m model) bool { return m.agents.hasNoAgentPane(m.unmeasured()) },
	}
}
