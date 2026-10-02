package ui

import (
	"fmt"
	"time"

	"komrad/config"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/view"

	tea "charm.land/bubbletea/v2"
)

// enter lands on the agent the target names, showing it if it is hidden, or comes back to the
// list as it stands.
func (a *agentsScreen) enter(m *model, target screenTarget) tea.Cmd {
	if target.agentID != "" {
		a.revealAgent(m, target.agentID)
	} else {
		a.cursor = cursor.Clamp(a.cursor, len(a.rows(m)))
	}

	a.reconcileRename(m)
	m.stageSelection()

	return nil
}

func (a *agentsScreen) update(m *model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if a.isRenaming() {
			return a.updateRename(m, msg)
		}
	case reviewPausedMsg:
		return a.applyPaused(m, msg)
	case tea.KeyPressMsg:
		a.reconcileRename(m)
		if a.isRenaming() {
			return a.updateRename(m, msg)
		}

		return a.list().update(m, msg, a.actions())
	}

	return nil
}

func (a *agentsScreen) list() screenList[agentRow] {
	return screenList[agentRow]{
		cursor: func() *rowCursor { return &a.rowCursor },
		rows:   a.rows,
		name:   agentRowName,
		refs:   refsOf,
		header: "folder",
		placed: a.stage,
		moved:  func(m *model) tea.Cmd { return m.waitToStage() },
		quit:   func(k config.Keybindings) []string { return k.Agents.Quit },
		back:   func(k config.Keybindings) []string { return k.Agents.Back },
	}
}

// stage puts the pane of whatever the cursor is standing on beside the list.
func (a *agentsScreen) stage(m *model) {
	m.stageSelection()
}

func (m *model) archiveAgents(agents []panel.Agent) tea.Cmd {
	if len(agents) == 0 {
		return nil
	}

	pending := m.unstageAhead(agents)

	return pending.attach(m.writeCmd(
		"archiving "+describeAgents(agents),
		fmt.Sprintf("Could not archive %s", describeAgents(agents)),
		func(a api) error {
			return pending.noteArchive(a.archiveAgents(panel.IDs(agents)))
		},
	))
}

func (m *model) archiveAndDeleteAgents(agents []panel.Agent) tea.Cmd {
	if len(agents) == 0 {
		return nil
	}

	ids := panel.IDs(agents)

	pending := m.unstageAhead(agents)

	return pending.attach(m.writeCmd(
		"archiving and deleting "+describeAgents(agents),
		fmt.Sprintf("Could not archive and delete %s", describeAgents(agents)),
		func(a api) error {
			if err := pending.noteArchive(a.archiveAgents(ids)); err != nil {
				return err
			}

			return a.deleteAgents(ids)
		},
	))
}

// openHovered attaches to whatever the cursor is on, which for a workflow is the orchestrator
// standing behind it.
func (a *agentsScreen) openHovered(m *model) tea.Cmd {
	if workflow, ok := a.hoveredWorkflow(m); ok {
		return startOrchestrator(m, workflow)
	}

	return a.openAgent(m)
}

func (a *agentsScreen) openAgent(m *model) tea.Cmd {
	item, ok := a.selectedAgent(m)
	if !ok {
		return nil
	}

	if _, waking := a.resuming[item.Agent.ID]; waking {
		return nil
	}

	if !item.Alive && !item.Agent.State.DoesPaneDecide() {
		return nil
	}

	if item.Alive {
		if item.Agent.HeldAt == nil {
			a.seeFinished(m, item)
			m.selectGuest(item.Agent.Pane)
		}

		return nil
	}

	return a.resume(m, item.Agent)
}

// resume wakes a dormant agent in the background.
func (a *agentsScreen) resume(m *model, agent panel.Agent) tea.Cmd {
	if a.resuming == nil {
		a.resuming = map[string]struct{}{}
	}
	a.resuming[agent.ID] = struct{}{}

	return stampResume(m.announcedCmd(
		"resuming "+agent.Name,
		fmt.Sprintf("Could not resume agent %q", agent.Name),
		message{
			kind:    messageInfo,
			summary: agent.Name + " is back up",
			detail:  "Press enter on it to open its pane.",
		},
		func(c api) error {
			_, err := c.resumeAgent(agent.ID)

			return err
		},
	), agent.ID)
}

// stampResume says on a write's result which agent it resumed.
func stampResume(cmd tea.Cmd, id string) tea.Cmd {
	return func() tea.Msg {
		msg := cmd()

		result, ok := msg.(writeResult)
		if !ok {
			return msg
		}

		result.resumed = id

		return result
	}
}

func (a *agentsScreen) settleResume(id string) {
	delete(a.resuming, id)
}

func (a *agentsScreen) toggleHidden(m *model) {
	selected := a.list().hovered(m)

	a.showHidden = !a.showHidden

	rows := a.rows(m)
	a.cursor = cursor.Place(rowIndexOfKey(rows, selected), a.cursor, len(rows))
	a.selected = a.selected.pruned(refsOf(a.rows(m)))
	a.stage(m)
}

// seeFinished snoozes the agent's family when it waits only to be looked at and the tree draws the
// agent, keeping the cursor on the agent's tree row.
func (a *agentsScreen) seeFinished(m *model, item panel.AgentRow) {
	treeKey := toTreeRowKey(a.list().hovered(m))
	if rowIndexOfKey(a.rows(m), treeKey) < 0 {
		return
	}

	a.triage.seeFinished(item, m.listing.agents)

	rows := a.rows(m)
	a.cursor = cursor.Place(rowIndexOfKey(rows, treeKey), a.cursor, len(rows))
}

// toggleSnooze snoozes or wakes the triage family of the hovered agent, or of the orchestrator behind
// a hovered workflow.
func (a *agentsScreen) toggleSnooze(m *model) {
	item, ok := a.hoveredPaneItem(m)
	if !ok {
		return
	}

	selected := a.list().hovered(m)
	slot := a.findZoneSlot(m)
	if !a.triage.toggleSnooze(item, m.listing.agents) {
		return
	}

	rows := a.rows(m)
	a.cursor = cursor.Place(rowIndexOfKey(rows, selected), a.cursor, len(rows))
	if slot >= 0 {
		a.moveAfterSnooze(m, slot)
	}

	a.stage(m)
}

func hideAgent(m *model, agent panel.Agent) tea.Cmd {
	doing, failure := "hiding "+agent.Name, fmt.Sprintf("Could not hide agent %q", agent.Name)
	if agent.Hidden {
		doing, failure = "unhiding "+agent.Name, fmt.Sprintf("Could not unhide agent %q", agent.Name)
	}

	return m.writeCmd(doing, failure, func(a api) error { return a.setHidden(agent, !agent.Hidden) })
}

func (a *agentsScreen) details(m *model) details {
	r, ok := a.hoveredRow(m)
	if !ok {
		return details{}
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, m.listing.agentCount(r.workflow.ID)),
		}
	}

	return details{
		id:      r.item.Agent.ID,
		notices: agentNotices(m.glyphs, m.loading.frame(), r),
		fields:  agentDetailFields(m, r.item),
		tags:    agentDetailsTags(m, r.item),
	}
}

// agentDetailFields is the record for an agent as the listing holds it.
func agentDetailFields(m *model, item panel.AgentRow) []recordField {
	worktree, read := agentWorktree(m.inspections, item.Agent.Cwd)

	return agentFields(agentFieldsParams{
		agent:        item.Agent,
		schedule:     m.listing.scheduleName(item.Agent),
		parent:       m.listing.parentName(item.Agent),
		workflow:     m.listing.workflowName(item.Agent.WorkflowID),
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
func (a *agentsScreen) doc(m *model) string {
	r, ok := a.hoveredRow(m)
	item, held := r.paneItem()

	switch {
	case !ok:
		return ""
	case r.header && !held:
		return workflowContextMarkdown(r.workflow)
	case item.Agent.State == panel.StateSettingUp:
		return settingUpPlaceholder
	case item.Agent.HeldAt != nil:
		return heldPlaceholder
	case item.Agent.State.IsFailed():
		return failedPlaceholder(item.Agent)
	}

	return ""
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

const heldPlaceholder = "Paused."

const stillPaused = "it is still paused"

func welcomeText(keys config.Keybindings) string {
	return fmt.Sprintf("Welcome, komrad!\n\nPress '%s' then '%s' to set up your first workflow", firstKey(keys.Menu.Open, "space"), firstKey(keys.Menu.NewWorkflow, "w"))
}

func firstKey(keys []string, fallback string) string {
	if len(keys) == 0 {
		return fallback
	}

	return keyLabel(keys[:1])
}

const settingUpPlaceholder = "_setting up…_\n\nMaking this agent's worktree. It has no pane yet."

func failedPlaceholder(agent panel.Agent) string {
	label := "setup"
	if agent.State == panel.StateFailedToCleanup {
		label = "teardown"
	}

	reason := agent.FailureReason
	if reason == "" {
		reason = "no reason was recorded"
	}

	return fmt.Sprintf("_%s failed_\n\n%s", label, reason)
}

func (a *agentsScreen) view(m *model) string {
	layout := a.buildLayout(m)
	a.drawn = &layout
	defer func() { a.drawn = nil }()

	if m.listing.isEmpty() {
		return m.welcomeView(a.listParams(m), welcomeText(m.keys))
	}

	return m.columnListView(a.listParams(m))
}

func agentsTabs() tabStrip {
	return tabStrip{
		title:    "f",
		legend:   "sort",
		options:  tabOptionsOf(agentSorts),
		fallback: "created",
		grouped:  "workflow",
	}
}

func (a *agentsScreen) listParams(m *model) screenListParams {
	return screenListParams{
		help:     a.help,
		rows:     len(a.rows(m)),
		snapshot: func(pane listPane) view.Snapshot { return a.snapshot(m, pane) },
	}
}

func (a *agentsScreen) help(m *model) keyMap {
	return a.actions().help(m)
}

func (a *agentsScreen) snapshot(m *model, pane listPane) view.Snapshot {
	layout := a.layout(m)

	return agentsSnapshot(snapshotParams{
		rows: layout.tree,
		triage: triageSnapshot{
			height:  a.triage.capacity,
			zone:    layout.zone,
			waiting: layout.waiting,
		},
		selected:     a.selected,
		scheduled:    m.listing.scheduledWorkflows(),
		workflowDirs: m.listing.workflowDirs(),
		focused:      rowKeyAt(layout.cursorRows(), a.cursor),
		caret:        pane.caret,
		blinkedOut:   pane.blinkedOut,
		loaderFrame:  m.loading.frame(),
		matcher:      m.filter.pattern,
		renaming:     a.renamingField(),
		showHidden:   a.showHidden,
		flat:         a.sort.flat,
		pane:         pane.width,
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

// workflowDirs is the directory each workflow launches its agents in, keyed by workflow id.
func (l listing) workflowDirs() map[string]string {
	dirs := make(map[string]string, len(l.workflows))
	for _, w := range l.workflows {
		dirs[w.ID] = w.Cwd
	}

	return dirs
}

// startOrchestrator brings the workflow's orchestrator back and puts the keyboard in its pane.
func startOrchestrator(m *model, workflow panel.Workflow) tea.Cmd {
	return m.rowWriteCmd(
		"starting the orchestrator of "+workflow.Name,
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
				agent:   agent.GetId(),
				pane:    agent.GetPaneId(),
				harness: agent.GetHarness(),
				focus:   true,
			}, nil
		},
	)
}

var (
	_ recordDrawer         = (*agentsScreen)(nil)
	_ listFilterer         = (*agentsScreen)(nil)
	_ tabSwitcher          = (*agentsScreen)(nil)
	_ screenLeaver         = (*agentsScreen)(nil)
	_ rowStager            = (*agentsScreen)(nil)
	_ hoveredNamer         = (*agentsScreen)(nil)
	_ screenMenuBuilder    = (*agentsScreen)(nil)
	_ itemMenuBuilder      = (*agentsScreen)(nil)
	_ loadingReporter      = (*agentsScreen)(nil)
	_ lineTyper            = (*agentsScreen)(nil)
	_ renamer              = (*agentsScreen)(nil)
	_ editorReconciler     = (*agentsScreen)(nil)
	_ hoveredAgentReporter = (*agentsScreen)(nil)
	_ savedScreenDrawer    = (*agentsScreen)(nil)
	_ agentPauser          = (*agentsScreen)(nil)
)

func (a *agentsScreen) drawsSwitcher() bool {
	return true
}

func (a *agentsScreen) doesDrawFromListing() bool {
	return true
}

func (a *agentsScreen) takesEveryKey() bool {
	return a.isRenaming()
}

func (a *agentsScreen) detailsQuit(keys config.Keybindings) []string {
	return keys.Agents.Quit
}

func (a *agentsScreen) detailsFit(m *model) bool {
	return m.listFitsDetails(a.listParams(m))
}

func (a *agentsScreen) detailsRoom(m *model) int {
	return m.listRecordRoom(a.listParams(m))
}

func (a *agentsScreen) hoveredKey(m *model) string {
	return a.list().hovered(m)
}

func (a *agentsScreen) hoveredName(m *model) string {
	return a.list().hoveredName(m)
}

func (a *agentsScreen) placeAt(m *model, key string) {
	a.list().place(m, key)
}

func (a *agentsScreen) filterRows(m *model) []filterRow {
	return a.list().filterRows(m)
}

func (a *agentsScreen) tabs() tabStrip {
	return agentsTabs()
}

func (a *agentsScreen) tabsState() *tabsState {
	return &a.sort
}

func (a *agentsScreen) tabsFit(m *model) bool {
	return m.listFitsTabs(a.listParams(m))
}

func (a *agentsScreen) column() columnSpec {
	return columnSpec{
		floor: columnFloor,
		read:  isAlwaysRead,
		docRows: func(m *model, width int) []string {
			return m.withLastScreen(paint.DocStyledRows(m.agents.doc(m), max(width, 1), columnStyle()), width)
		},
		holds: func(m *model) bool { return !m.agents.isEditorOpen() && m.agents.hasNoAgentPane(m) },
	}
}

// isEmpty reports whether there is no workflow and no agent to list.
func (l listing) isEmpty() bool {
	return len(l.workflows) == 0 && len(l.agents) == 0
}

func (a *agentsScreen) openRename(m *model) tea.Cmd {
	row, ok := a.hoveredRow(m)
	if !ok {
		return nil
	}

	a.renaming = renameAgentRow(row)

	return nil
}

func (a *agentsScreen) reconcileRename(m *model) {
	reconcileRename(&a.renameSlot, a.rows(m), a.hoveredKey(m), agentRowName)
}

func (a *agentsScreen) settleRename(m *model, row string, err error) {
	if a.settleField(row, err) {
		a.reconcileRename(m)
	}
}

func (a *agentsScreen) dispatches(*model) []dispatching {
	return []dispatching{a.actions()}
}
