package agents

import (
	"fmt"

	"komrad/internal/ui/api"
	"komrad/internal/ui/column"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/tabs"
	"komrad/internal/view"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// Enter lands on the task or agent the target names, showing the hidden tasks when it works on
// one, or comes back to the list as it stands.
func (a *Screen) Enter(in screen.Inputs, target screen.Target) screen.Request {
	switch {
	case target.TaskID != "":
		a.revealTask(in, target.TaskID, target.AgentID)
	case target.AgentID != "":
		a.revealAgent(in, target.AgentID)
	default:
		a.Row = cursor.Clamp(a.Row, len(a.rows(in)))
	}

	a.reconcileRename(in)

	return screen.Restage{}
}

// Update sends a key to the rename being typed, or to the screen's table.
func (a *Screen) Update(in screen.Inputs, msg tea.Msg) screen.Request {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		if a.IsRenaming() {
			return a.updateRename(a.deps.Register, a.deps.Clients, msg)
		}
	case tea.KeyPressMsg:
		a.reconcileRename(in)
		if a.IsRenaming() {
			return a.updateRename(a.deps.Register, a.deps.Clients, msg)
		}

		return a.actions(in).DispatchTop(a.deps.Top, a, a.deps.Keys, msg.String())
	}

	return nil
}

// Receive takes the editor's pane, a settled resume or rename, and the listing moving.
func (a *Screen) Receive(in screen.Inputs, msg tea.Msg) (screen.Request, bool) {
	switch msg := msg.(type) {
	case agentEditorStaged:
		return a.applyEditorStaged(msg), true
	case resumeSettled:
		return a.settleResume(in, msg.agentID, msg.err), true
	case api.RenameSettled:
		a.settleRename(in, msg.Row, msg.Err)

		return nil, true
	case screen.Relisted:
		return a.relist(in, msg), true
	}

	return nil, false
}

func (a *Screen) list() keys.ScreenList[agentRow] {
	return keys.ScreenList[agentRow]{
		Keys:   a.deps.Keys,
		Cursor: func() *list.Cursor { return &a.Cursor },
		Rows:   a.rows,
		Name:   agentRowName,
		Fits:   agentRowFits,
		Refs:   refsOf,
		Header: "folder",
		Placed: func(screen.Inputs) screen.Request { return a.Stage() },
		Moved:  func(screen.Inputs) screen.Request { return screen.Settle{} },
	}
}

// Stage puts the pane of whatever the cursor is standing on beside the list.
func (a *Screen) Stage() screen.Request {
	return screen.Restage{}
}

func (a *Screen) archiveAgents(agents []panel.Agent) screen.Request {
	if len(agents) == 0 {
		return nil
	}

	return a.deps.Clients.ArchivingWrite(api.ArchivingWriteParams{
		Doing:   "archiving " + describeAgents(agents),
		Summary: fmt.Sprintf("Could not archive %s", describeAgents(agents)),
		Agents:  agents,
		Write: func(clients api.Clients) error {
			return clients.ArchiveAgents(panel.IDs(agents))
		},
	})
}

// OpenHovered attaches to whatever the cursor is on, which for a workflow is the orchestrator
// standing behind it and for a task its writer.
func (a *Screen) OpenHovered(in screen.Inputs) screen.Request {
	if workflow, ok := a.hoveredWorkflow(in); ok {
		return a.startOrchestrator(workflow)
	}

	item, ok := a.hoveredPaneItem(in)
	if !ok {
		return nil
	}
	if a.isHoveringNotes(in) {
		return screen.Batch{a.closeNotes(), a.openAgent(in, item)}
	}

	return a.openAgent(in, item)
}

func (a *Screen) openAgent(in screen.Inputs, item panel.AgentRow) screen.Request {
	if _, waking := a.resuming[item.Agent.ID]; waking {
		return nil
	}

	if !item.Alive && !panel.DoesPaneDecide(item.Agent.State) {
		return nil
	}

	if item.Alive {
		a.seeFinished(in, item)

		return screen.SelectGuest{Pane: item.Agent.Pane}
	}

	return a.resume(item.Agent)
}

// resume wakes a dormant agent in the background.
func (a *Screen) resume(agent panel.Agent) screen.Request {
	if a.resuming == nil {
		a.resuming = map[string]struct{}{}
	}
	a.resuming[agent.ID] = struct{}{}

	clients := a.deps.Clients

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

func (a *Screen) settleResume(in screen.Inputs, id string, err error) screen.Request {
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
func (a *Screen) relist(in screen.Inputs, relisted screen.Relisted) screen.Request {
	before := relisted.Before(in)
	treeRow := a.list().Hovered(before)
	triageSlot := a.findZoneSlot(before)
	archiveShown := a.archiveOfKind()
	var archivedRow string
	if archiveShown != nil {
		archivedRow = archiveShown.list().Hovered(before)
	}

	a.triage.refresh(in.Listing.Agents, in.Listing.Tasks, a.deps.Clock.Now())
	a.apply(in, treeRow)
	followed := a.followAwaitingCLI(in)
	a.landInTriage(in, treeRow, triageSlot)
	if archiveShown != nil {
		archiveShown.place(in, archivedRow)
	}

	return followed
}

func (a *Screen) followAwaitingCLI(in screen.Inputs) screen.Request {
	var notes screen.Batch
	for id, paneSeen := range a.awaitingCLI {
		item, ok := in.Listing.LiveAgent(id)

		switch {
		case !ok:
			delete(a.awaitingCLI, id)
		case item.Alive && panel.DoesPaneDecide(item.Agent.State):
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

func (a *Screen) toggleHidden(in screen.Inputs) screen.Request {
	selected := a.list().Hovered(in)

	a.showHidden = !a.showHidden

	rows := a.rows(in)
	a.Row = cursor.Place(list.IndexOf(rows, selected), a.Row, len(rows))
	a.Selected = a.Selected.Pruned(refsOf(a.rows(in)))

	return a.Stage()
}

// seeFinished snoozes the agent's task when it waits only to be looked at and the tree renders the
// agent, keeping the cursor on the agent's tree row.
func (a *Screen) seeFinished(in screen.Inputs, item panel.AgentRow) {
	treeKey := toTreeRowKey(a.list().Hovered(in))
	if list.IndexOf(a.rows(in), treeKey) < 0 {
		return
	}

	a.triage.seeFinished(item, in.Listing.Agents)

	rows := a.rows(in)
	a.Row = cursor.Place(list.IndexOf(rows, treeKey), a.Row, len(rows))
}

// toggleSnooze snoozes or wakes the task under the key in triage, keeping the cursor in the zone.
func (a *Screen) toggleSnooze(in screen.Inputs, key string) screen.Request {
	selected := a.list().Hovered(in)
	slot := a.findZoneSlot(in)
	if !a.triage.toggleSnooze(key, in.Listing.Agents) {
		return nil
	}

	rows := a.rows(in)
	a.Row = cursor.Place(list.IndexOf(rows, selected), a.Row, len(rows))
	var moved screen.Request
	if slot >= 0 {
		moved = a.moveAfterSnooze(in, slot)
	}

	return screen.Batch{moved, a.Stage()}
}

// hasPane reports whether an agent has a pane of its own worth putting in the window.
func hasPane(item panel.AgentRow) bool {
	return item.Alive &&
		item.Agent.State != agentv1.AgentState_AGENT_STATE_SETTING_UP &&
		!panel.IsFailed(item.Agent.State)
}

func agentsTabs() tabs.Strip {
	return tabs.Strip{
		Title:        "f",
		Legend:       "sort",
		Options:      tabs.OptionsOf(agentSorts),
		Fallback:     "created",
		GroupedLabel: "workflow",
	}
}

// ListParams is the agents list as the root renders it.
func (a *Screen) ListParams(in screen.Inputs) list.Params {
	params := list.Params{
		Help:     a.help(in),
		Rows:     len(a.rows(in)),
		Snapshot: func(pane list.Pane) view.Snapshot { return a.snapshot(in, pane) },
	}
	if in.Listing.IsEmpty() {
		params.Welcome = welcomeText(a.deps.Keys)
	}

	return params
}

func (a *Screen) help(in screen.Inputs) legend.Help {
	return a.actions(in).Help(a, a.deps.Keys)
}

// startOrchestrator brings the workflow's orchestrator back and puts the keyboard in its pane.
func (a *Screen) startOrchestrator(workflow panel.Workflow) screen.Request {
	return rowWrite(a.deps.Clients,
		"starting the orchestrator of "+workflow.Name,
		fmt.Sprintf("Could not reach the orchestrator of workflow %q", workflow.Name),
		func(clients api.Clients) screen.Landed {
			agent, err := clients.StartOrchestrator(workflow.ID)
			if err != nil {
				return screen.Landed{Err: err}
			}

			// The orchestrator has no row of its own, so the landing goes to the workflow it
			// stands behind.
			return madeWorkflow(workflow.ID, agent)
		},
	)
}

// DoesRenderFromListing reports that the screen renders the listing's agents and tasks.
func (a *Screen) DoesRenderFromListing() bool {
	return true
}

// TakesEveryKey reports whether a rename is taking the keys.
func (a *Screen) TakesEveryKey() bool {
	return a.IsRenaming()
}

// ActiveList is the agents list the filter narrows.
func (a *Screen) ActiveList() keys.KeyedList {
	return a.list()
}

// HoveredName is the name of the row under the cursor.
func (a *Screen) HoveredName(in screen.Inputs) string {
	return a.list().HoveredName(in)
}

// Tabs is the strip of columns the list can be ordered by.
func (a *Screen) Tabs() tabs.Strip {
	return agentsTabs()
}

// TabsState is where the screen keeps the column chosen and whether the strip holds the keys.
func (a *Screen) TabsState() *tabs.State {
	return &a.sort
}

// Column is the hovered row's notes rendered beside the list.
func (a *Screen) Column(in screen.Inputs) column.Spec {
	return column.Spec{
		Floor: column.Narrowest,
		Read:  column.IsAlwaysRead,
		DocRows: func(width int) []string {
			return paint.DocStyledRows(a.doc(in), max(width, 1), paint.Panel.Stage())
		},
		Said: func() string {
			if a.isHoveringNotes(in) {
				return ""
			}

			return a.HoveredAgentID(in)
		},
		Holds: func() bool {
			return !a.IsEditorOpen() && (a.hasNoAgentPane(in) || in.Room.Pattern.HasSaid(a.HoveredAgentID(in)))
		},
	}
}

func (a *Screen) openRename(in screen.Inputs) screen.Request {
	row, ok := a.hoveredRow(in)
	if !ok {
		return nil
	}

	a.renaming = renameAgentRow(row)

	return nil
}

func (a *Screen) reconcileRename(in screen.Inputs) {
	reconcileRename(&a.renameSlot, a.rows(in), a.list().Hovered(in), agentRowName)
}

func (a *Screen) settleRename(in screen.Inputs, row string, err error) {
	if a.settleField(row, err) {
		a.reconcileRename(in)
	}
}

// Dispatches is the screen's table and its archive's.
func (a *Screen) Dispatches() []keys.Table {
	return []keys.Table{a.actions(screen.Inputs{}), a.archiveScreen(&a.archived).actions(screen.Inputs{})}
}

// StageResting restages the row the cursor came to rest on and reads its agent's saved screen.
func (a *Screen) StageResting(screen.Inputs) screen.Request {
	return screen.Batch{screen.Restage{}, screen.ReadLastScreen{}}
}
