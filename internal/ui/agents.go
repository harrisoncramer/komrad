package ui

import (
	"fmt"
	"time"

	"weave/config"
	"weave/internal/view"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
)

func agentsList() listSpec[agentRow] {
	return listSpec[agentRow]{
		cursor: func(m *model) *rowCursor { return &m.tree.rowCursor },
		rows:   model.agentRows,
		name:   agentRowName,
		refs:   refsOf,
		header: "folder",
		placed: (*model).stageSelection,
		moved:  model.deferStaging,
		quit:   func(k config.Keybindings) []string { return k.Agents.Quit },
		back:   func(k config.Keybindings) []string { return k.Agents.Back },
	}
}

func agentsChrome() listChrome {
	return listChrome{
		title: screenTitle(view.GlyphWorkflow, "Agents & Workflows"),
		help:  model.agentHelp,
	}
}

func (m model) updateAgents(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	return agentsList().update(m, key, agentsActions())
}

func (m model) withSelectedAgent(fn func(agentItem) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	return fn(item)
}

func (m model) archiveAgentsConfirm(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	form, answer := archiveAgentsForm(agents, unnamedChildren(m.liveAgents(), agents), formKeys())

	return m, m.openForm(formState{
		kind:   formAgentArchive,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			switch *answer {
			case archiveChoiceArchive:
				return m.archiveAgents(agents)
			case archiveChoiceArchiveDelete:
				return m.archiveAndDeleteAgents(agents)
			case archiveChoiceCancel:
				return m, nil
			}

			return m, nil
		}),
	})
}

func (m model) archiveAgents(agents []agentstore.Agent) (model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.releasePanes(agents)

	return m, m.writeCmd(
		fmt.Sprintf("Could not archive %s", describeAgents(agents)),
		func(a api) error { return a.archiveAgents(agentstore.IDs(agents)) },
	)
}

func (m model) archiveAndDeleteAgents(agents []agentstore.Agent) (model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.releasePanes(agents)

	ids := agentstore.IDs(agents)

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
func (m model) openHovered() (tea.Model, tea.Cmd) {
	if workflow, ok := m.hoveredWorkflow(); ok {
		return m.startOrchestrator(workflow)
	}

	return m.openAgent()
}

func (m model) openAgent() (tea.Model, tea.Cmd) {
	return m.withSelectedAgent(func(item agentItem) (tea.Model, tea.Cmd) {
		if !item.alive && !item.agent.State.DoesPaneDecide() {
			return m, nil
		}

		if item.alive {
			if item.agent.HeldAt != nil {
				return m, nil
			}

			if pane := m.frame.GuestPane(); pane != "" && pane == item.agent.Pane {
				m.tmux.SelectPane(pane)
			}

			return m, nil
		}

		return m, m.resumeCmd(resumeParams{
			summary: fmt.Sprintf("Could not resume agent %q", item.agent.Name),
			id:      item.agent.ID,
		})
	})
}

func (m model) toggleHiddenAgents() (tea.Model, tea.Cmd) {
	selected := rowKeyAt(m.agentRows(), m.tree.cursor)

	m.tree.showHidden = !m.tree.showHidden

	rows := m.agentRows()
	if idx := rowIndexOfKey(rows, selected); idx >= 0 {
		m.tree.cursor = idx
	} else {
		m.tree.cursor = clampCursor(m.tree.cursor, len(rows))
	}
	m.tree.selected = m.tree.selected.pruned(refsOf(m.agentRows()))
	m.stageSelection()

	return m, nil
}

func (m model) hideAgent(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	failure := fmt.Sprintf("Could not hide agent %q", agent.Name)
	if agent.Hidden {
		failure = fmt.Sprintf("Could not unhide agent %q", agent.Name)
	}

	return m, m.writeCmd(failure, func(a api) error { return a.setHidden(agent, !agent.Hidden) })
}

// unpauseSaid is what an unpaused agent is told.
const unpauseSaid = "I paused you for a moment and have let you go now. " +
	"Look at your worktree as it stands before carrying on. It may have changed while you were paused."

// pauseAgent stops the agent where it stands, or starts one already paused.
func (m model) pauseAgent(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	if agent.HeldAt != nil {
		return m, m.releaseCmd(fmt.Sprintf("Could not unpause agent %q", agent.Name), agent, unpauseSaid)
	}

	return m, m.writeCmd(fmt.Sprintf("Could not pause agent %q", agent.Name), func(a api) error {
		return a.hold(agent)
	})
}

// releaseInSilenceConfirm lets an agent go without typing at it.
func (m model) releaseInSilenceConfirm(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	form, confirmed := releaseInSilenceForm(agent.Name, formKeys())

	return m, m.openForm(formState{
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

func (m model) agentsDetails() details {
	r, ok := rowAt(m.agentRows(), m.tree.cursor)
	if !ok {
		return details{}
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			fields: workflowFields(r.workflow, m.agentCount(r.workflow.ID)),
		}
	}

	return details{
		id:      r.item.agent.ID,
		notices: agentNotices(r),
		fields:  m.agentDetailFields(r.item),
	}
}

// agentDetailFields is the record for an agent as the listing holds it.
func (m model) agentDetailFields(item agentItem) []recordField {
	worktree, read := m.agentWorktree(item.agent.Cwd)

	return agentFields(agentFieldsParams{
		agent:    item.agent,
		schedule: m.scheduleName(item.agent),
		parent:   m.parentName(item.agent),
		standing: item.pullRequest,
		listed:   m.pullRequests.listed[item.agent.ID],
		worktree: worktree,
		read:     read,
		now:      time.Now(),
	})
}

// agentsColumn is what stands where a running agent's own pane would.
func agentsColumn() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   model.agentDoc,
		holds: model.hasNoAgentPane,
	}
}

// hasNoAgentPane reports whether the row the cursor is on has no pane of its own.
func (m model) hasNoAgentPane() bool {
	r, ok := rowAt(m.agentRows(), m.tree.cursor)
	if !ok {
		return true
	}

	item, held := r.paneItem()
	if !held {
		return true
	}

	return !hasPane(item)
}

// hasPane reports whether an agent has a pane of its own worth putting in the window.
func hasPane(item agentItem) bool {
	return item.alive &&
		item.agent.HeldAt == nil &&
		item.agent.State != agentstore.StateSettingUp &&
		!item.agent.State.IsFailed()
}

// agentDoc is the markdown drawn beside the list for a row with no pane of its own.
func (m model) agentDoc() string {
	r, ok := rowAt(m.agentRows(), m.tree.cursor)
	item, held := r.paneItem()

	switch {
	case !ok:
		return m.noAgentsMarkdown()
	case r.header && !held:
		return workflowContextMarkdown(r.workflow)
	case item.agent.State == agentstore.StateSettingUp:
		return settingUpPlaceholder
	case item.agent.HeldAt != nil:
		return heldPlaceholder
	case item.agent.State.IsFailed():
		return m.failedPlaceholder(item.agent)
	default:
		return dormantPlaceholder
	}
}

// parentName is what the agent a child is attached to is called.
func (m model) parentName(agent agentstore.Agent) string {
	if agent.ParentAgentID == nil {
		return ""
	}

	if item, ok := m.liveAgent(*agent.ParentAgentID); ok {
		return item.agent.Name
	}

	return *agent.ParentAgentID
}

func (m model) scheduleName(agent agentstore.Agent) string {
	if agent.ScheduleID == nil {
		return ""
	}

	for _, item := range m.listing.schedules {
		if item.schedule.ID == *agent.ScheduleID {
			return item.schedule.Name
		}
	}

	return ""
}

const dormantPlaceholder = "_dormant — not running_\n\nPress open to resume."

const heldPlaceholder = "_paused_\n\nPress unpause to let it carry on, or unpause in silence to let it go without typing at it."

func (m model) noAgentsMarkdown() string {
	key := "w"
	if len(m.keys.Workflows.New) > 0 {
		key = m.keys.Workflows.New[0]
	}

	return fmt.Sprintf("# Agents\n\n_nothing here yet_\n\nPress %q to make a workflow and an agent in it.\n", key)
}

const settingUpPlaceholder = "_setting up…_\n\nMaking this agent's worktree. It has no pane yet."

// failureReasonKey is the metadata key a failed create or cleanup records its reason under.
const failureReasonKey = "error"

func (m model) failedPlaceholder(agent agentstore.Agent) string {
	// A create dies at the provisioner or at one of the pre-launch hooks after it, so the run
	// that failed is the newest of either rather than the newest provision run.
	label, moments := "setup", []string{config.TriggerProvision, config.HookPreLaunch}
	// A delete dies either at a pre-teardown hook or at the deprovisioner after it, so the run
	// that failed is the newest of the two.
	if agent.State == agentstore.StateFailedToCleanup {
		label, moments = "teardown", []string{config.HookPreTeardown, config.TriggerDeprovision}
	}

	if run, ok := m.latestRunLog(agent.ID, moments); ok {
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

func (m model) agentsView() string {
	m.tree.rows = m.buildAgentRows()

	return m.columnView(model.agentsListView)
}

func agentsTabs() tabStrip {
	return tabStrip{
		title:    "↑↓",
		legend:   "sort",
		options:  tabOptionsOf(agentSorts),
		fallback: "created",
	}
}

func (m model) agentsListView() string {
	return m.listView(listViewParams{
		chrome:   agentsChrome(),
		rows:     len(m.agentRows()),
		snapshot: model.agentsSnapshot,
	})
}

func (m model) agentsSnapshot(pane int) view.Snapshot {
	rows := m.agentRows()

	return agentsSnapshot(snapshotParams{
		rows:          rows,
		marks:         m.listing.marks,
		selected:      m.tree.selected,
		scheduled:     m.scheduledWorkflows(),
		focused:       rowKeyAt(rows, m.tree.cursor),
		caretHidden:   m.cursorBlinkedOut(m.isListFocused()),
		barBlinkedOut: m.rowCursor.Blink,
		matcher:       m.filter.pattern,
		showHidden:    m.tree.showHidden,
		pane:          pane,
	})
}

// scheduledWorkflows is the workflows at least one schedule launches into.
func (m model) scheduledWorkflows() map[string]bool {
	scheduled := make(map[string]bool, len(m.listing.schedules))
	for _, item := range m.listing.schedules {
		scheduled[item.schedule.WorkflowID] = true
	}

	return scheduled
}

// startOrchestrator brings the workflow's orchestrator back and puts the keyboard in its pane.
func (m model) startOrchestrator(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	return m, m.rowWriteCmd(
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
