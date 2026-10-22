package ui

import (
	"fmt"

	"komrad/internal/ui/cursor"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/view"

	tea "charm.land/bubbletea/v2"
)

// enter lands on the task or agent the target names, showing the hidden tasks when it works on
// one, or comes back to the list as it stands.
func (a *agentsScreen) enter(m *model, target screen.Target) tea.Cmd {
	switch {
	case target.TaskID != "":
		a.revealTask(m, target.TaskID, target.AgentID)
	case target.AgentID != "":
		a.revealAgent(m, target.AgentID)
	default:
		a.Row = cursor.Clamp(a.Row, len(a.rows(m)))
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
		cursor: func() *list.Cursor { return &a.Cursor },
		rows:   a.rows,
		name:   agentRowName,
		fits:   (*model).agentRowFits,
		refs:   refsOf,
		header: "folder",
		placed: a.stage,
		moved:  func(m *model) tea.Cmd { return m.waitToStage() },
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

	return m.archivingWriteCmd(archivingWriteCmdParams{
		doing:   "archiving " + describeAgents(agents),
		summary: fmt.Sprintf("Could not archive %s", describeAgents(agents)),
		agents:  agents,
		write: func(a api) error {
			return a.archiveAgents(panel.IDs(agents))
		},
	})
}

// openHovered attaches to whatever the cursor is on, which for a workflow is the orchestrator
// standing behind it and for a task its writer.
func (a *agentsScreen) openHovered(m *model) tea.Cmd {
	if workflow, ok := a.hoveredWorkflow(m); ok {
		return startOrchestrator(m, workflow)
	}

	item, ok := a.hoveredPaneItem(m)
	if !ok {
		return nil
	}
	if a.isHoveringNotes(m) {
		a.closeNotes(m)
	}

	return a.openAgent(m, item)
}

func (a *agentsScreen) openAgent(m *model, item panel.AgentRow) tea.Cmd {
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

	clients := m.api

	return m.apply(screen.Write{
		Doing:   "resuming " + agent.Name,
		Summary: fmt.Sprintf("Could not resume agent %q", agent.Name),
		Send: func() screen.Landed {
			_, err := clients.resumeAgent(agent.ID)

			return screen.Landed{Err: err, Then: resumeSettled{agentID: agent.ID, err: err}}
		},
	})
}

// resumeSettled is a resume landed, for the agents list.
type resumeSettled struct {
	agentID string
	err     error
}

func (a *agentsScreen) settleResume(m *model, id string, err error) {
	delete(a.resuming, id)
	if err != nil {
		return
	}

	if a.awaitingCLI == nil {
		a.awaitingCLI = map[string]bool{}
	}
	a.awaitingCLI[id] = false
	a.followAwaitingCLI(m)
}

func (a *agentsScreen) followAwaitingCLI(m *model) {
	for id, paneSeen := range a.awaitingCLI {
		item, ok := m.listing.LiveAgent(id)

		switch {
		case !ok:
			delete(a.awaitingCLI, id)
		case item.Alive && item.Agent.State.DoesPaneDecide():
			delete(a.awaitingCLI, id)
			m.pushMessage(message{
				kind:    messageInfo,
				summary: item.Agent.Name + " is back up",
				detail:  "Press enter on it to open its pane.",
			})
		case item.Agent.Pane != "":
			a.awaitingCLI[id] = true
		case paneSeen:
			delete(a.awaitingCLI, id)
		}
	}
}

func (a *agentsScreen) toggleHidden(m *model) {
	selected := a.list().hovered(m)

	a.showHidden = !a.showHidden

	rows := a.rows(m)
	a.Row = cursor.Place(list.IndexOf(rows, selected), a.Row, len(rows))
	a.Selected = a.Selected.Pruned(refsOf(a.rows(m)))
	a.stage(m)
}

// seeFinished snoozes the agent's task when it waits only to be looked at and the tree renders the
// agent, keeping the cursor on the agent's tree row.
func (a *agentsScreen) seeFinished(m *model, item panel.AgentRow) {
	treeKey := toTreeRowKey(a.list().hovered(m))
	if list.IndexOf(a.rows(m), treeKey) < 0 {
		return
	}

	a.triage.seeFinished(item, m.listing.Agents)

	rows := a.rows(m)
	a.Row = cursor.Place(list.IndexOf(rows, treeKey), a.Row, len(rows))
}

// toggleSnooze snoozes or wakes the task under the key in triage, keeping the cursor in the zone.
func (a *agentsScreen) toggleSnooze(m *model, key string) {
	selected := a.list().hovered(m)
	slot := a.findZoneSlot(m)
	if !a.triage.toggleSnooze(key, m.listing.Agents) {
		return
	}

	rows := a.rows(m)
	a.Row = cursor.Place(list.IndexOf(rows, selected), a.Row, len(rows))
	if slot >= 0 {
		a.moveAfterSnooze(m, slot)
	}

	a.stage(m)
}

// hasPane reports whether an agent has a pane of its own worth putting in the window.
func hasPane(item panel.AgentRow) bool {
	return item.Alive &&
		item.Agent.HeldAt == nil &&
		item.Agent.State != panel.StateSettingUp &&
		!item.Agent.State.IsFailed()
}

const heldPlaceholder = "Paused."

const stillPaused = "it is still paused"

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
		snapshot: func(pane list.Pane) view.Snapshot { return a.snapshot(m, pane) },
	}
}

func (a *agentsScreen) help(m *model) keyMap {
	return m.helpOf(a.actions())
}

// startOrchestrator brings the workflow's orchestrator back and puts the keyboard in its pane.
func startOrchestrator(m *model, workflow panel.Workflow) tea.Cmd {
	return m.rowWriteCmd(
		"starting the orchestrator of "+workflow.Name,
		fmt.Sprintf("Could not reach the orchestrator of workflow %q", workflow.Name),
		func(a api) (*screen.Made, error) {
			agent, err := a.startOrchestrator(workflow.ID)
			if err != nil {
				return nil, err
			}

			// The orchestrator has no row of its own, so the landing goes to the workflow it
			// stands behind.
			return &screen.Made{
				Row:     workflowRowKey(workflow.ID),
				Agent:   agent.GetId(),
				Pane:    agent.GetPaneId(),
				Harness: agent.GetHarness(),
				Focus:   true,
			}, nil
		},
	)
}

var (
	_ recordRenderer       = (*agentsScreen)(nil)
	_ activeLister         = (*agentsScreen)(nil)
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
	_ savedScreenRenderer  = (*agentsScreen)(nil)
	_ agentPauser          = (*agentsScreen)(nil)
)

func (a *agentsScreen) doesRenderFromListing() bool {
	return true
}

func (a *agentsScreen) takesEveryKey() bool {
	return a.isRenaming()
}

func (a *agentsScreen) detailsFit(m *model) bool {
	return m.listFitsDetails(a.listParams(m))
}

func (a *agentsScreen) detailsRoom(m *model) int {
	return m.listRecordRoom(a.listParams(m))
}

func (a *agentsScreen) activeList() keyedList {
	return a.list()
}

func (a *agentsScreen) hoveredName(m *model) string {
	return a.list().hoveredName(m)
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
			if !m.agents.isHoveringNotes(m) {
				if rows := m.saidColumnRows(m.agents.hoveredAgentID(m), width); rows != nil {
					return rows
				}
			}

			return m.withLastScreen(paint.DocStyledRows(m.agents.doc(m), max(width, 1), columnStyle()), width)
		},
		holds: func(m *model) bool {
			return !m.agents.isEditorOpen() && (m.agents.hasNoAgentPane(m) || m.filter.hasSaid(m.agents.hoveredAgentID(m)))
		},
	}
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
	reconcileRename(&a.renameSlot, a.rows(m), a.list().hovered(m), agentRowName)
}

func (a *agentsScreen) settleRename(m *model, row string, err error) {
	if a.settleField(row, err) {
		a.reconcileRename(m)
	}
}

func (a *agentsScreen) dispatches(*model) []keys.Table {
	return []keys.Table{a.actions(), a.archived.actions()}
}
