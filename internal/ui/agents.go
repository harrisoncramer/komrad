package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/view"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

var errNoWorkflow = errors.New("every agent belongs to a workflow, so create one first")

var errChildOfChild = errors.New("a child agent may not have children of its own")

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
		func(a api) error { return a.archiveAgents(agentIDs(agents)) },
	)
}

func (m model) archiveAndDeleteAgents(agents []agentstore.Agent) (model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.releasePanes(agents)

	ids := agentIDs(agents)

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

// releasePanes swaps the given agents' panes out of the stage and marks them locally as no
// longer alive.
func (m *model) releasePanes(agents []agentstore.Agent) {
	released := make(map[string]bool, len(agents))
	for _, agent := range agents {
		m.frame.ReleaseGuest(agent.Pane)
		released[agent.ID] = true
	}

	for i, item := range m.listing.agents {
		if released[item.agent.ID] {
			m.listing.agents[i].alive = false
		}
	}
}

// stageDelay is how long a cursor has to rest before the panes are moved to what it is standing
// on.
const stageDelay = 90 * time.Millisecond

// stageTickMsg is the rest a cursor move waited for, carrying the move it was owed to.
type stageTickMsg struct {
	move int
}

// deferStaging waits for the cursor to come to rest before the panes are moved.
func (m model) deferStaging() (model, tea.Cmd) {
	m.staging++
	move := m.staging

	return m, tea.Tick(stageDelay, func(time.Time) tea.Msg { return stageTickMsg{move: move} })
}

// applyStageTick stages what the cursor came to rest on and asks for what the screen reads of it.
func (m *model) applyStageTick(msg stageTickMsg) tea.Cmd {
	if msg.move != m.staging {
		return nil
	}

	if m.screen == screenWorktrees {
		m.stageWorktrees()

		return m.inspectWorktreeCmd()
	}

	m.stageSelection()

	return tea.Batch(m.listPullRequestsCmd(), m.inspectAgentWorktreeCmd(time.Now()))
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

// sizeAgentWindows keeps every agent's own window at the size of the pane it is shown in.
func (m *model) sizeAgentWindows() {
	if m.frame.IsRemote() {
		return
	}

	width, height := m.frame.StageCols(), m.height
	if width <= 0 || height <= 0 {
		return
	}

	if m.sized == nil {
		m.sized = map[string]string{}
	}

	want := fmt.Sprintf("%dx%d", width, height)
	guest := m.frame.GuestPane()

	for _, item := range m.listing.agents {
		pane := item.agent.Pane

		// The guest is standing in the panel's own window, so resizing its window would resize the
		// terminal weave is drawn in.
		if pane == "" || pane == guest || m.sized[pane] == want {
			continue
		}

		m.tmux.ResizeWindow(pane, width, height)
		m.sized[pane] = want
	}
}

// stageSelection puts the pane of whatever the cursor is standing on beside the list.
func (m *model) stageSelection() {
	m.frame.Begin()
	defer m.afterStaging()

	rows := m.agentRows()
	m.placeColumn(screenAgents, rowKeyAt(rows, m.tree.cursor))
	m.reconcileSolo()

	r, ok := rowAt(rows, m.tree.cursor)
	staged, held := r.paneItem()

	switch {
	case ok && held && !m.hasNoAgentPane() && m.isPaneStageable(staged.agent.Pane, staged.agent.Harness):
		m.sizeGuestWindow(staged.agent.Pane)
		m.frame.ShowGuest(staged.agent.Pane)
	case m.frame.IsSolo() && m.isGuestLive():
	default:
		m.frame.Show(layout.RegionStage, newBlankContent())
	}

	m.stageInfo()
}

// afterStaging sends the pane moves staging collected.
func (m *model) afterStaging() {
	m.frame.Flush()
}

// sizeGuestWindow puts an agent's own window at the size of the stage before it is swapped in.
func (m *model) sizeGuestWindow(pane string) {
	// A remote agent's window is sized by the ssh client attached to it, which is the stage pane
	// itself, so there is nothing here to set and the pane id names a window on another server.
	if m.frame.IsRemote() {
		return
	}

	width, height := m.frame.StageCols(), m.height
	if pane == "" || pane == m.frame.GuestPane() || width <= 0 || height <= 0 {
		return
	}

	if m.sized == nil {
		m.sized = map[string]string{}
	}

	want := fmt.Sprintf("%dx%d", width, height)
	if m.sized[pane] == want {
		return
	}

	m.tmux.ResizeWindow(pane, width, height)
	m.sized[pane] = want
}

// isGuestLive reports whether the pane parked in the stage still belongs to an agent the last
// listing found running.
func (m model) isGuestLive() bool {
	guest := m.frame.GuestPane()
	if guest == "" {
		return true
	}

	for _, item := range m.listing.agents {
		if item.agent.Pane == guest {
			return item.alive
		}
	}

	return false
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

	for _, item := range m.schedules.items {
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
	scheduled := make(map[string]bool, len(m.schedules.items))
	for _, item := range m.schedules.items {
		scheduled[item.schedule.WorkflowID] = true
	}

	return scheduled
}

func (m model) openNewAgent(workflow *workflowstore.Workflow) (tea.Model, tea.Cmd) {
	if workflow == nil {
		m.report("Could not start an agent", errNoWorkflow)

		return m, nil
	}

	values := agentPrefill(*workflow)

	return m.openAgentForm(*workflow, &values)
}

// agentPrefill is the new-agent form as the workflow leaves it, before anything is typed.
func agentPrefill(workflow workflowstore.Workflow) agentFormValues {
	return agentFormValues{
		Harness:  workflow.Harness,
		Dir:      workflow.Cwd,
		Worktree: inheritWorktree,
	}
}

func (m model) openAgentForm(workflow workflowstore.Workflow, values *agentFormValues) (tea.Model, tea.Cmd) {
	return m.openRedrawnForm(openRedrawnFormParams{
		Kind: formAgent,
		Build: func(m model) (*huh.Form, []string) {
			return newAgentForm(formKeys(), values, workflow.Worktree.Enabled)
		},
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   func(m model) (tea.Model, tea.Cmd) { return m.submitAgent(workflow, *values) },
	})
}

// openNewChild opens the form that attaches a child to the hovered agent.
func (m model) openNewChild(parent agentstore.Agent) (tea.Model, tea.Cmd) {
	if parent.ParentAgentID != nil {
		m.report("Could not attach a child", errChildOfChild)

		return m, nil
	}

	values := childPrefill(parent)

	return m.openChildForm(parent, &values)
}

// childPrefill is the child form as the parent leaves it.
func childPrefill(parent agentstore.Agent) agentFormValues {
	return agentFormValues{Harness: parent.Harness}
}

func (m model) openChildForm(parent agentstore.Agent, values *agentFormValues) (tea.Model, tea.Cmd) {
	return m.openRedrawnForm(openRedrawnFormParams{
		Kind:     formChild,
		Build:    func(m model) (*huh.Form, []string) { return childAgentForm(formKeys(), values, parent) },
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   func(m model) (tea.Model, tea.Cmd) { return m.submitChild(parent, *values) },
	})
}

// submitChild launches the child without a directory or a provisioner.
func (m model) submitChild(parent agentstore.Agent, values agentFormValues) (tea.Model, tea.Cmd) {
	cmd := m.createCmd(createAgentParams{
		Prompt:        values.Prompt,
		Title:         values.Title,
		Harness:       values.Harness,
		Flags:         values.Flags,
		WorkflowID:    parent.WorkflowID,
		ParentAgentID: parent.ID,
	})

	return m, tea.Batch(cmd, m.holdForm())
}

func (m model) openRetryForm(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	values := retryFormValues{
		Prompt:  agent.Prompt,
		Harness: agent.Harness,
		Flags:   agent.Flags,
	}

	return m.openRedrawnForm(openRedrawnFormParams{
		Kind:     formAgentRetry,
		Build:    func(m model) (*huh.Form, []string) { return retryAgentForm(formKeys(), &values) },
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   func(m model) (tea.Model, tea.Cmd) { return m.submitRetry(agent, values) },
	})
}

func (m model) submitRetry(agent agentstore.Agent, values retryFormValues) (tea.Model, tea.Cmd) {
	cmd := m.retryCmd(agent, values)

	return m, tea.Batch(cmd, m.holdForm())
}

func (m model) openRename(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	return m.openRenameForm(agent, new(string))
}

func (m model) openRenameForm(agent agentstore.Agent, name *string) (tea.Model, tea.Cmd) {
	return m.openRedrawnForm(openRedrawnFormParams{
		Kind:   formRename,
		Build:  func(m model) (*huh.Form, []string) { return renameForm(agent.Name, name, formKeys()) },
		Submit: func(m model) (tea.Model, tea.Cmd) { return m.submitRename(agent, *name) },
	})
}

func (m model) openAdopt(workflow *workflowstore.Workflow) (tea.Model, tea.Cmd) {
	if workflow == nil {
		m.report("Could not adopt", errNoWorkflow)

		return m, nil
	}

	values := adoptPrefill(*workflow)

	return m.openAdoptForm(*workflow, &values)
}

// adoptPrefill is the adopt form as the workflow leaves it, before anything is typed.
func adoptPrefill(workflow workflowstore.Workflow) adoptFormValues {
	return adoptFormValues{Harness: workflow.Harness}
}

func (m model) openAdoptForm(workflow workflowstore.Workflow, values *adoptFormValues) (tea.Model, tea.Cmd) {
	return m.openRedrawnForm(openRedrawnFormParams{
		Kind:     formAdopt,
		Build:    func(m model) (*huh.Form, []string) { return adoptAgentForm(formKeys(), values) },
		Editable: []editableText{{key: "prompt", value: &values.Prompt}},
		Submit:   func(m model) (tea.Model, tea.Cmd) { return m.submitAdopt(workflow, *values) },
	})
}

func (m model) submitAdopt(workflow workflowstore.Workflow, values adoptFormValues) (tea.Model, tea.Cmd) {
	cmd := m.adoptCmd(adoptParams{
		Ref:        values.Ref,
		Prompt:     values.Prompt,
		Title:      values.Title,
		Harness:    values.Harness,
		Flags:      values.Flags,
		WorkflowID: workflow.ID,
	})

	return m, tea.Batch(cmd, m.holdForm())
}

func (m model) submitRename(agent agentstore.Agent, typed string) (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(typed)
	cmd := m.writeCmd(
		fmt.Sprintf("Could not rename agent %q", agent.Name),
		func(a api) error { return a.renameAgent(agent, name) },
	)

	return m, tea.Batch(cmd, m.holdForm())
}

func (m model) submitAgent(workflow workflowstore.Workflow, values agentFormValues) (tea.Model, tea.Cmd) {
	cmd := m.createCmd(createAgentParams{
		Prompt:           values.Prompt,
		Title:            values.Title,
		Harness:          values.Harness,
		Dir:              m.currentDir(values.Dir),
		Flags:            values.Flags,
		ProvisionEnabled: worktreeOverride(values.Worktree),
		WorkflowID:       workflow.ID,
	})

	return m, tea.Batch(cmd, m.holdForm())
}

// worktreeOverride is what the form says about the agent's directory: nil to follow the
// workflow, false to keep the directory it was given.
func worktreeOverride(selected string) *bool {
	if selected == withoutWorktree {
		return xptr.Ptr(false)
	}

	return nil
}

// openRefreshConfirm asks before bringing a worktree up to date.
func (m model) openRefreshConfirm(plan refreshPlan) (tea.Model, tea.Cmd) {
	form, confirmed := refreshForm(plan, formKeys())

	return m, m.openForm(formState{
		kind:   formAgentRefresh,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, m.rebaseCmd(plan.agentID, plan.name)
		}),
	})
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
