package ui

import (
	"fmt"
	"time"

	"weave/config"
	"weave/internal/ui/panel"
	"weave/internal/view"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
)

// enter lands on the agent the target names, showing it if it is hidden, or comes back to the
// list as it stands.
func (a *agentsScreen) enter(h host, s panelShared, target screenTarget) tea.Cmd { //nolint:unparam // the door's shape
	if target.agentID != "" {
		a.revealAgent(s, target.agentID)
	} else {
		a.cursor = clampCursor(a.cursor, len(a.rows(s)))
	}

	h.stageSelection()

	return nil
}

func (a *agentsScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	key, ok := msg.(tea.KeyMsg)
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

	if !item.alive && !item.agent.State.DoesPaneDecide() {
		return nil
	}

	if item.alive {
		if item.agent.HeldAt == nil {
			h.selectGuest(item.agent.Pane)
		}

		return nil
	}

	return h.openingWriteCmd(fmt.Sprintf("Could not resume agent %q", item.agent.Name), func(c api) (*agentv1.Agent, error) {
		return c.resumeAgent(item.agent.ID)
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
		id:      r.item.agent.ID,
		notices: agentNotices(r),
		fields:  agentDetailFields(s, r.item),
	}
}

// agentDetailFields is the record for an agent as the listing holds it.
func agentDetailFields(s panelShared, item agentItem) []recordField {
	worktree, read := agentWorktree(s.inspections, item.agent.Cwd)

	return agentFields(agentFieldsParams{
		agent:    item.agent,
		schedule: s.listing.scheduleName(item.agent),
		parent:   s.listing.parentName(item.agent),
		workflow: s.listing.workflowName(item.agent.WorkflowID),
		standing: item.pullRequest,
		listed:   s.pullRequests.listed[item.agent.ID],
		worktree: worktree,
		read:     read,
		now:      time.Now(),
	})
}

// hasPane reports whether an agent has a pane of its own worth putting in the window.
func hasPane(item agentItem) bool {
	return item.alive &&
		item.agent.HeldAt == nil &&
		item.agent.State != panel.StateSettingUp &&
		!item.agent.State.IsFailed()
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
	case item.agent.State == panel.StateSettingUp:
		return settingUpPlaceholder
	case item.agent.HeldAt != nil:
		return heldPlaceholder
	case item.agent.State.IsFailed():
		return failedPlaceholder(s, item.agent)
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
		return item.agent.Name
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

// agentsAdapter is the agents screen behind the panel's one door.
type agentsAdapter struct{}

var (
	_ detailed = agentsAdapter{}
	_ filtered = agentsAdapter{}
	_ tabbed   = agentsAdapter{}
	_ columned = agentsAdapter{}
	_ staged   = agentsAdapter{}
)

func (agentsAdapter) update(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.agents.update(&m, m.shared(), msg)

	return m, cmd
}

func (agentsAdapter) view(m model) string {
	return m.agents.view(m.shared())
}

func (agentsAdapter) needs() screenNeeds {
	return screenNeeds{fromListing: true}
}

func (agentsAdapter) open(m model, target screenTarget) (tea.Model, tea.Cmd) {
	m.enterScreen(screenAgents)
	cmd := m.agents.enter(&m, m.shared(), target)

	return m, cmd
}

func (agentsAdapter) takesEveryKey(model) bool {
	return false
}

func (agentsAdapter) details(m model) details {
	return m.agents.details(m.shared())
}

func (agentsAdapter) detailsQuit(keys config.Keybindings) []string {
	return keys.Agents.Quit
}

func (agentsAdapter) detailsFit(m model) bool {
	s := m.shared()

	return s.draw.listFitsDetails(m.agents.listParams(s))
}

func (agentsAdapter) detailsRoom(m model) int {
	s := m.shared()

	return s.draw.listRecordRoom(m.agents.listParams(s))
}

func (agentsAdapter) hoveredKey(m model) string {
	return m.agents.list().hovered(m.shared())
}

func (agentsAdapter) placeAt(m *model, key string) {
	m.agents.list().place(m, m.shared(), key)
}

func (agentsAdapter) filterRows(m model) []filterRow {
	return m.agents.list().filterRows(m.shared())
}

func (agentsAdapter) tabs() tabStrip {
	return agentsTabs()
}

func (agentsAdapter) tabsState(m *model) *tabsState {
	return &m.agents.sort
}

func (agentsAdapter) tabsFit(m model) bool {
	s := m.shared()

	return s.draw.listFitsTabs(m.agents.listParams(s))
}

func (agentsAdapter) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   func(m model) string { return m.agents.doc(m.shared()) },
		holds: func(m model) bool { return m.agents.hasNoAgentPane(m.unmeasured()) },
	}
}

func (agentsAdapter) stage(m *model) {
	m.agents.stage(m, m.shared())
}
