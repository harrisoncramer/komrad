package ui

import (
	"fmt"

	"komrad/internal/ui/api"
	"komrad/internal/ui/column"
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
func (a *agentsScreen) enter(m *model, in screen.Inputs, target screen.Target) screen.Request {
	switch {
	case target.TaskID != "":
		a.revealTask(m, in, target.TaskID, target.AgentID)
	case target.AgentID != "":
		a.revealAgent(m, in, target.AgentID)
	default:
		a.Row = cursor.Clamp(a.Row, len(a.rows(m, in)))
	}

	a.reconcileRename(m, in)

	return screen.Restage{}
}

func (a *agentsScreen) update(m *model, in screen.Inputs, msg tea.Msg) screen.Request {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if a.isRenaming() {
			return a.updateRename(a.deps.register, a.deps.clients, msg)
		}
	case tea.KeyPressMsg:
		a.reconcileRename(m, in)
		if a.isRenaming() {
			return a.updateRename(a.deps.register, a.deps.clients, msg)
		}

		return a.list().update(m, msg, a.actions())
	}

	return nil
}

func (a *agentsScreen) receive(m *model, in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case reviewPausedMsg:
		if !in.Up {
			return nil, true
		}

		return a.applyPaused(msg), true
	case agentEditorStaged:
		return a.applyEditorStaged(in, msg), true
	case resumeSettled:
		return a.settleResume(in, msg.agentID, msg.err), true
	case api.RenameSettled:
		a.settleRename(m, in, msg.Row, msg.Err)

		return nil, true
	case screen.Relisted:
		return a.relist(m, in, msg), true
	}

	return nil, false
}

func (a *agentsScreen) list() screenList[agentRow] {
	return screenList[agentRow]{
		keys:   a.deps.keys,
		top:    a.deps.top,
		cursor: func() *list.Cursor { return &a.Cursor },
		rows:   a.rows,
		name:   agentRowName,
		fits:   (*model).agentRowFits,
		refs:   refsOf,
		header: "folder",
		placed: a.stage,
		moved:  func(*model, screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

// stage puts the pane of whatever the cursor is standing on beside the list.
func (a *agentsScreen) stage(*model, screen.Inputs) screen.Request {
	return screen.Restage{}
}

func (a *agentsScreen) archiveAgents(agents []panel.Agent) screen.Request {
	if len(agents) == 0 {
		return nil
	}

	return a.deps.clients.ArchivingWrite(api.ArchivingWriteParams{
		Doing:   "archiving " + describeAgents(agents),
		Summary: fmt.Sprintf("Could not archive %s", describeAgents(agents)),
		Agents:  agents,
		Write: func(clients api.Clients) error {
			return clients.ArchiveAgents(panel.IDs(agents))
		},
	})
}

// openHovered attaches to whatever the cursor is on, which for a workflow is the orchestrator
// standing behind it and for a task its writer.
func (a *agentsScreen) openHovered(m *model, in screen.Inputs) screen.Request {
	if workflow, ok := a.hoveredWorkflow(m, in); ok {
		return a.startOrchestrator(workflow)
	}

	item, ok := a.hoveredPaneItem(m, in)
	if !ok {
		return nil
	}
	if a.isHoveringNotes(m) {
		return screen.Batch{a.closeNotes(), a.openAgent(m, in, item)}
	}

	return a.openAgent(m, in, item)
}

func (a *agentsScreen) openAgent(m *model, in screen.Inputs, item panel.AgentRow) screen.Request {
	if _, waking := a.resuming[item.Agent.ID]; waking {
		return nil
	}

	if !item.Alive && !item.Agent.State.DoesPaneDecide() {
		return nil
	}

	if item.Alive {
		if item.Agent.HeldAt == nil {
			a.seeFinished(m, in, item)

			return screen.SelectGuest{Pane: item.Agent.Pane}
		}

		return nil
	}

	return a.resume(item.Agent)
}

// resume wakes a dormant agent in the background.
func (a *agentsScreen) resume(agent panel.Agent) screen.Request {
	if a.resuming == nil {
		a.resuming = map[string]struct{}{}
	}
	a.resuming[agent.ID] = struct{}{}

	clients := a.deps.clients

	return screen.Write{
		Doing:   "resuming " + agent.Name,
		Summary: fmt.Sprintf("Could not resume agent %q", agent.Name),
		Send: func() screen.Landed {
			_, err := clients.ResumeAgent(agent.ID)

			return screen.Landed{Err: err, Then: resumeSettled{agentID: agent.ID, err: err}}
		},
	}
}

// resumeSettled is a resume landed, for the agents list.
type resumeSettled struct {
	agentID string
	err     error
}

func (a *agentsScreen) settleResume(in screen.Inputs, id string, err error) screen.Request {
	delete(a.resuming, id)
	if err != nil {
		return nil
	}

	if a.awaitingCLI == nil {
		a.awaitingCLI = map[string]bool{}
	}
	a.awaitingCLI[id] = false

	return a.followAwaitingCLI(in)
}

// relist puts the cursors of the tree, the zone and the archive on show back on the rows they were on.
func (a *agentsScreen) relist(m *model, in screen.Inputs, relisted screen.Relisted) screen.Request {
	before := relisted.Before(in)
	treeRow := a.list().hovered(m, before)
	triageSlot := a.findZoneSlot(m, before)
	archiveShown := a.shownArchive()
	var archivedRow string
	if archiveShown != nil {
		archivedRow = archiveShown.list().hovered(m, before)
	}

	a.triage.refresh(in.Listing.Agents, in.Listing.Tasks, a.deps.clock.Now())
	a.apply(m, in, treeRow)
	followed := a.followAwaitingCLI(in)
	a.landInTriage(m, in, treeRow, triageSlot)
	if archiveShown != nil {
		archiveShown.place(m, in, archivedRow)
	}

	return followed
}

func (a *agentsScreen) followAwaitingCLI(in screen.Inputs) screen.Request {
	var notes screen.Batch
	for id, paneSeen := range a.awaitingCLI {
		item, ok := in.Listing.LiveAgent(id)

		switch {
		case !ok:
			delete(a.awaitingCLI, id)
		case item.Alive && item.Agent.State.DoesPaneDecide():
			delete(a.awaitingCLI, id)
			notes = append(notes, screen.Note{
				Kind:    screen.Info,
				Summary: item.Agent.Name + " is back up",
				Detail:  "Press enter on it to open its pane.",
			})
		case item.Agent.Pane != "":
			a.awaitingCLI[id] = true
		case paneSeen:
			delete(a.awaitingCLI, id)
		}
	}

	return notes
}

func (a *agentsScreen) toggleHidden(m *model, in screen.Inputs) screen.Request {
	selected := a.list().hovered(m, in)

	a.showHidden = !a.showHidden

	rows := a.rows(m, in)
	a.Row = cursor.Place(list.IndexOf(rows, selected), a.Row, len(rows))
	a.Selected = a.Selected.Pruned(refsOf(a.rows(m, in)))

	return a.stage(m, in)
}

// seeFinished snoozes the agent's task when it waits only to be looked at and the tree renders the
// agent, keeping the cursor on the agent's tree row.
func (a *agentsScreen) seeFinished(m *model, in screen.Inputs, item panel.AgentRow) {
	treeKey := toTreeRowKey(a.list().hovered(m, in))
	if list.IndexOf(a.rows(m, in), treeKey) < 0 {
		return
	}

	a.triage.seeFinished(item, in.Listing.Agents)

	rows := a.rows(m, in)
	a.Row = cursor.Place(list.IndexOf(rows, treeKey), a.Row, len(rows))
}

// toggleSnooze snoozes or wakes the task under the key in triage, keeping the cursor in the zone.
func (a *agentsScreen) toggleSnooze(m *model, in screen.Inputs, key string) screen.Request {
	selected := a.list().hovered(m, in)
	slot := a.findZoneSlot(m, in)
	if !a.triage.toggleSnooze(key, in.Listing.Agents) {
		return nil
	}

	rows := a.rows(m, in)
	a.Row = cursor.Place(list.IndexOf(rows, selected), a.Row, len(rows))
	var moved screen.Request
	if slot >= 0 {
		moved = a.moveAfterSnooze(m, in, slot)
	}

	return screen.Batch{moved, a.stage(m, in)}
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

func (a *agentsScreen) listParams(m *model, in screen.Inputs) screenListParams {
	return screenListParams{
		help:     a.help,
		rows:     len(a.rows(m, in)),
		snapshot: func(pane list.Pane) view.Snapshot { return a.snapshot(m, in, pane) },
	}
}

func (a *agentsScreen) help(m *model) keyMap {
	return m.helpOf(a.actions())
}

// startOrchestrator brings the workflow's orchestrator back and puts the keyboard in its pane.
func (a *agentsScreen) startOrchestrator(workflow panel.Workflow) screen.Request {
	return rowWrite(a.deps.clients,
		"starting the orchestrator of "+workflow.Name,
		fmt.Sprintf("Could not reach the orchestrator of workflow %q", workflow.Name),
		func(clients api.Clients) (*screen.Made, error) {
			agent, err := clients.StartOrchestrator(workflow.ID)
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

func (a *agentsScreen) activeList() keyedList {
	return a.list()
}

func (a *agentsScreen) hoveredName(m *model, in screen.Inputs) string {
	return a.list().hoveredName(m, in)
}

func (a *agentsScreen) tabs() tabStrip {
	return agentsTabs()
}

func (a *agentsScreen) tabsState() *tabsState {
	return &a.sort
}

func (a *agentsScreen) column(m *model, in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: columnFloor,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			if !m.agents.isHoveringNotes(m) {
				if rows := m.saidColumnRows(m.agents.hoveredAgentID(m, in), width); rows != nil {
					return rows
				}
			}

			return m.withLastScreen(paint.DocStyledRows(m.agents.doc(m, in), max(width, 1), columnStyle()), width)
		},
		Holds: func() bool {
			return !m.agents.isEditorOpen() && (m.agents.hasNoAgentPane(m, in) || m.filter.hasSaid(m.agents.hoveredAgentID(m, in)))
		},
	}
}

func (a *agentsScreen) openRename(m *model, in screen.Inputs) screen.Request {
	row, ok := a.hoveredRow(m, in)
	if !ok {
		return nil
	}

	a.renaming = renameAgentRow(row)

	return nil
}

func (a *agentsScreen) reconcileRename(m *model, in screen.Inputs) {
	reconcileRename(&a.renameSlot, a.rows(m, in), a.list().hovered(m, in), agentRowName)
}

func (a *agentsScreen) settleRename(m *model, in screen.Inputs, row string, err error) {
	if a.settleField(row, err) {
		a.reconcileRename(m, in)
	}
}

func (a *agentsScreen) dispatches(*model) []keys.Table {
	return []keys.Table{a.actions(), a.archived.actions()}
}
