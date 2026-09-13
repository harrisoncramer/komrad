package ui

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"weave/config"
	"weave/internal/ui/layout"
	"weave/internal/ui/paint"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

var errNoWorkflow = errors.New("every agent belongs to a workflow, so create one first")

var errChildOfChild = errors.New("a child agent may not have children of its own")

func (m model) updateAgents(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// The doubled top key is a gesture rather than a binding, so it is settled before the table,
	// which would otherwise let a single press jump.
	if m.takeTopPress(k) {
		next, cmd := m.jumpCursor(0)

		return next, cmd
	}
	if m.topHeld {
		return m, nil
	}

	return agentsActions().dispatch(m, k)
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

func (m model) moveCursor(delta int) (model, tea.Cmd) {
	next, ok := m.tree.move(delta, len(m.agentRows()))
	if !ok {
		return m, nil
	}

	m.tree.rowCursor = next

	return m.deferStaging()
}

func (m model) jumpCursor(idx int) (model, tea.Cmd) {
	next, ok := m.tree.jump(idx, len(m.agentRows()))
	if !ok {
		return m, nil
	}

	m.tree.rowCursor = next

	return m.deferStaging()
}

// stageDelay is how long a cursor has to rest before the panes are moved to what it is standing
// on, which is under what a scroll between two rows takes.
const stageDelay = 90 * time.Millisecond

// stageTickMsg is the rest a cursor move waited for, carrying the move it was owed to so a later
// one takes precedence.
type stageTickMsg struct {
	move int
}

// deferStaging waits for the cursor to come to rest before the panes are moved, so scrolling a
// list is one change of layout rather than one for every row passed over.
func (m model) deferStaging() (model, tea.Cmd) {
	m.staging++
	move := m.staging

	return m, tea.Tick(stageDelay, func(time.Time) tea.Msg { return stageTickMsg{move: move} })
}

// applyStageTick stages what the cursor came to rest on, ignoring a rest the cursor has already
// moved on from.
func (m *model) applyStageTick(msg stageTickMsg) {
	if msg.move != m.staging {
		return
	}

	m.stageSelection()
}

// toggleAgentViewed puts an agent's unread mark back by hand, or takes it off, for the times
// weave's own reading of the pane is wrong about what happened.
func (m model) toggleAgentViewed(item agentItem) (tea.Model, tea.Cmd) {
	viewed := item.unviewed

	// A mark set by hand is held against the dwell until the cursor leaves the row.
	m.hover = hoverState{
		agentID: item.agent.ID,
		since:   time.Now(),
		held:    !viewed,
	}

	summary := fmt.Sprintf("Could not mark agent %q unread", item.agent.Name)
	if viewed {
		summary = fmt.Sprintf("Could not mark agent %q read", item.agent.Name)
	}

	return m, m.writeCmd(summary, func(a api) error { return a.setViewed(item.agent.ID, viewed) })
}

func (m model) openAgent() (tea.Model, tea.Cmd) {
	return m.withSelectedAgent(func(item agentItem) (tea.Model, tea.Cmd) {
		if !item.alive && !item.agent.State.DoesPaneDecide() {
			return m, nil
		}

		m.history.Visit(item.agent.ID)

		if item.alive {
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
	m.tree.selected = m.tree.selected.pruned(m.agentRows())
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
		return m, m.writeCmd(fmt.Sprintf("Could not unpause agent %q", agent.Name), func(a api) error {
			return a.release(agent, unpauseSaid)
		})
	}

	return m, m.writeCmd(fmt.Sprintf("Could not pause agent %q", agent.Name), func(a api) error {
		return a.hold(agent)
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
		notices: agentNotices(r.item),
		fields:  agentFields(r.item.agent, m.scheduleName(r.item.agent), m.parentName(r.item.agent)),
	}
}

// agentsColumn is what stands where a running agent's own pane would: a folder's shared notes, or
// why the row under the cursor has no pane to show.
func agentsColumn() columnSpec {
	return columnSpec{
		floor: columnFloor,
		doc:   model.agentDoc,
		holds: model.hasNoAgentPane,
	}
}

// hasNoAgentPane reports whether the row the cursor is on has no pane of its own, which is what
// leaves the window free for the panel to say why instead.
func (m model) hasNoAgentPane() bool {
	r, ok := rowAt(m.agentRows(), m.tree.cursor)
	if !ok || r.header {
		return true
	}

	return !r.item.alive ||
		r.item.agent.State == agentstore.StateSettingUp ||
		r.item.agent.State.IsFailed()
}

// agentDoc is the markdown drawn beside the list for a row with no pane of its own.
func (m model) agentDoc() string {
	r, ok := rowAt(m.agentRows(), m.tree.cursor)
	switch {
	case !ok:
		return noAgentsMarkdown
	case r.header:
		return workflowContextMarkdown(r.workflow)
	case r.item.agent.State == agentstore.StateSettingUp:
		return settingUpPlaceholder
	case r.item.agent.State.IsFailed():
		return m.failedPlaceholder(r.item.agent)
	default:
		return dormantPlaceholder
	}
}

// sizeAgentWindows keeps every agent's own window at the size of the pane it is shown in.
func (m *model) sizeAgentWindows() {
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

// stageSelection puts the pane of whatever the cursor is standing on beside the list, which for a
// running agent is the agent's own and for everything else is nothing: the panel takes the window
// and says why there is no pane in its own frame instead.
func (m *model) stageSelection() {
	m.frame.Begin()
	defer m.afterStaging()

	rows := m.agentRows()
	m.placeColumn(screenAgents, rowKeyAt(rows, m.tree.cursor))
	m.reconcileSolo()

	switch r, ok := rowAt(rows, m.tree.cursor); {
	case ok && !m.hasNoAgentPane() && m.isPaneStageable(r.item.agent.Pane, r.item.agent.Harness):
		m.sizeGuestWindow(r.item.agent.Pane)
		m.frame.ShowGuest(r.item.agent.Pane)
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

// sizeGuestWindow puts an agent's own window at the size of the stage before it is swapped in,
// which sizeAgentWindows cannot do for the pane already standing there.
func (m *model) sizeGuestWindow(pane string) {
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
// listing found running, which is what says it can be left there while the panel covers it.
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

// parentName is what the agent a child is attached to is called, which is what the details show
// rather than the id the row already carries.
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

// noAgentsMarkdown is what stands beside an empty list.
const noAgentsMarkdown = "# Agents\n\n_nothing here yet_\n\nPress new to make a workflow and an agent in it.\n"

const settingUpPlaceholder = "_setting up…_\n\nMaking this agent's worktree. It has no pane yet."

// failureReasonKey is the metadata key a failed create or cleanup records its reason under,
// left out of agentFields since failedPlaceholder is what shows it.
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
	return m.columnView(model.agentsListView)
}

func (m model) agentsListView() string {
	record, help := m.detailsView(), m.renderHelp(m.agentHelp())
	below := record + m.messagesRegion(record, help) + help

	all := m.agentRows()
	rows := make([]string, 0, len(all))
	for i, r := range all {
		hovered := i == m.tree.cursor
		prefix := "  "
		if hovered {
			prefix = m.rowCursorPrefix()
		}

		if r.header {
			label := m.filter.highlight(clipName(r.workflow.Name), m.nameStyle(hovered, false))
			header := prefix + paint.MarkGutter(m.listing.marks[rowKey(r)]) + label + hiddenTally(r.hiddenCount, m.tree.showHidden)
			rows = append(rows, header)

			continue
		}

		item := r.item
		name := m.filter.highlight(clipName(item.agent.Name), m.nameStyle(hovered, !item.alive))
		row := prefix + paint.MarkGutter(m.listing.marks[rowKey(r)]) + childMark(r.child) + stateMark(displayState(item)) + " " + name + " " + worktreeRemovedMark(item) + heldMark(item.agent) + scheduledMark(item.agent) + hiddenMark(item.agent.Hidden) + unreadMailMark(item.unreadMail) + unviewedMark(item.unviewed) + failedRunMark(item.failedRun) + localReviewRoundsMark(item.localRounds) + localReviewMark(item.inLocalReview, item.localVerdict) + hiddenTally(r.hiddenCount, m.tree.showHidden) + reasonMark(item.agent)
		if m.tree.selected.has(item.agent.ID) {
			row = paint.PaintRow(row, paint.SelectionBackground, m.width)
		}

		rows = append(rows, row)
	}

	return m.listView("Agents and Workflows", rows, m.tree.cursor, below)
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
		Flags:    workflow.Flags,
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

// openNewChild attaches a child to the hovered agent, saying why on a child rather than opening
// a form whose submit the service could only refuse.
func (m model) openNewChild(parent agentstore.Agent) (tea.Model, tea.Cmd) {
	if parent.ParentAgentID != nil {
		m.report("Could not attach a child", errChildOfChild)

		return m, nil
	}

	values := agentFormValues{Harness: parent.Harness, Flags: parent.Flags}

	return m.openChildForm(parent, &values)
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
	return adoptFormValues{Harness: workflow.Harness, Flags: workflow.Flags}
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

// worktreeOverride reads the one thing an agent can say for itself about its directory: unset
// to follow the workflow, or false to keep the directory it was given.
func worktreeOverride(selected string) *bool {
	if selected == withoutWorktree {
		return xptr.Ptr(false)
	}

	return nil
}

const prMetadataKey = "pr"

func hasPR(agent agentstore.Agent) bool {
	for _, entry := range agent.Metadata {
		if entry.Key == prMetadataKey {
			return entry.Value != ""
		}
	}

	return false
}

// refreshAgent asks the server what a rebase would do, then offers the picker.
func (m model) refreshAgent(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	if !m.isReviewable(agent) {
		return m, nil
	}

	agentID, name := agent.ID, agent.Name

	return m, m.planRefreshCmd(agentID, name)
}

// openRefreshConfirm asks before bringing a worktree up to date, saying what it will do to the
// branch and to the files.
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

func (m model) openAgentPR(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	if !hasPR(agent) {
		return m, nil
	}

	agentID := agent.ID

	return m, m.writeCmd(
		"Could not open the pull request",
		func(a api) error { return a.openPullRequest(agentID) },
	)
}
