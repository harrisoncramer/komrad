package ui

import (
	"errors"
	"fmt"
	"strings"

	"weave/config"
	"weave/internal/ui/layout"

	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var errNoWorkflow = errors.New("every agent belongs to a workflow, so create one first")

func (m model) updateAgents(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	if m.takeTopPress(k) {
		return m.jumpCursor(0), nil
	}
	if m.topHeld {
		return m, nil
	}
	if isBound(k, m.keys.Nav.Bottom) {
		return m.jumpCursor(len(m.agentRows()) - 1), nil
	}

	if sel, took := m.applySelectionKey(k, m.tree.selected, m.agentRows(), m.tree.cursor); took {
		m.tree.selected = sel

		return m, nil
	}

	if m.tree.selected.active() {
		return m.updateAgentsSelection(key)
	}

	return m.updateAgentsBrowsing(key)
}

func (m model) updateAgentsBrowsing(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	if isBound(k, m.keys.Filter.Open) {
		return m.openFilter()
	}
	if m.filter.standing() && isBound(k, m.keys.Agents.Back) {
		return m.openFilter()
	}
	if isBound(k, m.keys.Workflows.New) {
		return m.newWorkflowForm()
	}
	if isBound(k, m.keys.Agents.Search) {
		return m.enterSearch()
	}
	if isBound(k, m.keys.Schedules.Open) {
		return m.enterSchedules()
	}
	if isBound(k, m.keys.Archive.Open) {
		return m.enterArchive()
	}
	if isBound(k, m.keys.Agents.ShowHidden) {
		return m.toggleHiddenAgents()
	}
	if workflow, ok := m.hoveredWorkflow(); ok {
		return m.updateHoveredWorkflow(key, workflow)
	}

	switch {
	case isBound(k, m.keys.Agents.Quit), isBound(k, m.keys.Agents.Back):
		return m, m.hide()
	case isBound(k, m.keys.Agents.New):
		return m.openNewAgent(m.enclosingWorkflow())
	case isBound(k, m.keys.Nav.Up):
		return m.moveCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveCursor(1), nil
	case isBound(k, m.keys.Agents.Open):
		return m.openAgent()
	case isBound(k, m.keys.Actions.Open):
		return m.enterActions()
	}

	return m.updateHoveredAgent(key)
}

func (m model) updateHoveredAgent(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	return m.withSelectedAgent(func(item agentItem) (tea.Model, tea.Cmd) {
		switch {
		case isBound(k, m.keys.Agents.Tag):
			return m.tagAgent(item.agent)
		case isBound(k, m.keys.Agents.Hide):
			return m.hideAgent(item.agent)
		case isBound(k, m.keys.Agents.OpenPR):
			return m.openAgentPR(item.agent)
		case isBound(k, m.keys.Agents.Diff):
			return m.openAgentDiff(item.agent)
		case isBound(k, m.keys.Agents.Rename):
			m.target.renameAgent = item.agent
			m.form, m.values.rename = renameForm(item.agent.Name, "", m.keys.Form)
			m.screen = screenRename

			return m, m.form.Init()
		case isBound(k, m.keys.Agents.Archive):
			return m.archiveAgentsConfirm([]agentstore.Agent{item.agent})
		}

		return m, nil
	})
}

func (m model) withSelectedAgent(fn func(agentItem) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}

	return fn(item)
}

func (m model) updateAgentsSelection(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, m.keys.Agents.Quit):
		return m, m.hide()
	case isBound(k, m.keys.Agents.Tag):
		return m.tagSelection()
	case isBound(k, m.keys.Agents.Archive):
		return m.archiveAgentsConfirm(chosenAgents(m.tree.selected, m.allAgentRows()))
	case isBound(k, m.keys.Agents.ShowHidden):
		return m.toggleHiddenAgents()
	case isBound(k, m.keys.Nav.Up):
		return m.moveCursor(-1), nil
	case isBound(k, m.keys.Nav.Down):
		return m.moveCursor(1), nil
	}

	return m, nil
}

func (m model) archiveAgentsConfirm(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.target.archiveAgents = agents
	m.form, m.values.archive = archiveAgentsForm(agents, m.keys.Form)
	m.screen = screenAgentArchive

	return m, m.form.Init()
}

func (m model) updateAgentArchive(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateConfirmForm(msg, confirmFormActions{
		write: func(m model) (model, tea.Cmd) {
			target := m.target.archiveAgents
			if len(target) == 0 {
				return m, nil
			}

			switch m.archiveAnswer() {
			case archiveChoiceArchive:
				return m.archiveAgents(target)
			case archiveChoiceArchiveDelete:
				return m.archiveAndDeleteAgents(target)
			case archiveChoiceCancel:
				return m, nil
			}

			return m, nil
		},
		clear: func(m model) model {
			m.target.archiveAgents = nil
			m.values.archive = nil

			return m
		},
		returnTo: func(m model) (tea.Model, tea.Cmd) {
			return m.returnToAgents()
		},
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

// releasePanes swaps the given agents' real panes out of the stage and marks them locally as
// no longer alive. The mark matters because returnToAgents calls stageSelection synchronously,
// before any refresh has told the model these agents are gone; without it, stageSelection sees
// the cursor still on a just-released row and swaps the same pane straight back in, right as
// the delete that follows kills it out from under the frame.
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

func (m model) moveCursor(delta int) model {
	next, ok := m.tree.move(delta, len(m.agentRows()))
	if !ok {
		return m
	}

	m.tree.rowCursor = next
	m.stageSelection()

	return m
}

func (m model) jumpCursor(idx int) model {
	next, ok := m.tree.jump(idx, len(m.agentRows()))
	if !ok {
		return m
	}

	m.tree.rowCursor = next
	m.stageSelection()

	return m
}

func (m model) openAgent() (tea.Model, tea.Cmd) {
	return m.withSelectedAgent(func(item agentItem) (tea.Model, tea.Cmd) {
		if item.alive || item.agent.State == agentstore.StateSettingUp || agentstore.IsFailed(item.agent.State) {
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
	m.tree.selected = m.tree.selected.pruned(m.allAgentRows())
	m.stageSelection()

	return m, nil
}

func (m model) tagAgent(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	next := nextTag(agent.Tag, m.tags)

	return m, m.writeCmd(
		fmt.Sprintf("Could not tag agent %q", agent.Name),
		func(a api) error { return a.setTags([]string{agent.ID}, next) },
	)
}

func (m model) tagSelection() (tea.Model, tea.Cmd) {
	ids := m.tree.selected.ids
	next := nextSelectionTag(selectionTags(m.tree.selected, m.allAgentRows()), m.tags)

	return m, m.writeCmd(
		fmt.Sprintf("Could not tag %d agents", len(ids)),
		func(a api) error { return a.setTags(ids, next) },
	)
}

func (m model) hideAgent(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	failure := fmt.Sprintf("Could not hide agent %q", agent.Name)
	if agent.Hidden {
		failure = fmt.Sprintf("Could not unhide agent %q", agent.Name)
	}

	return m, m.writeCmd(failure, func(a api) error { return a.setHidden(agent, !agent.Hidden) })
}

func (m model) agentsDetails() details {
	r, ok := rowAt(m.agentRows(), m.tree.cursor)
	if !ok {
		return details{}
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			kind:   "Workflow",
			title:  r.workflow.Name,
			fields: workflowFields(r.workflow, m.agentCount(r.workflow.ID)),
		}
	}

	return details{
		id:     r.item.agent.ID,
		kind:   "Agent",
		title:  r.item.agent.Name,
		fields: agentFields(r.item.agent, m.scheduleName(r.item.agent)),
	}
}

func (m *model) stageSelection() {
	r, ok := rowAt(m.agentRows(), m.tree.cursor)
	if !ok {
		m.frame.Show(layout.RegionStage, newBlankContent())
		m.stageInfo()

		return
	}

	switch {
	case r.header:
		m.stageDocument(workflowContextMarkdown(r.workflow))
	case r.item.agent.State == agentstore.StateSettingUp:
		m.stageDocument(settingUpPlaceholder)
	case agentstore.IsFailed(r.item.agent.State):
		m.stageDocument(m.failedPlaceholder(r.item.agent))
	case r.item.alive:
		m.frame.ShowGuest(r.item.agent.Pane)
	default:
		m.stageDocument(dormantPlaceholder)
	}

	m.stageInfo()
}

func nextTag(current *string, tags []config.Tag) *string {
	if len(tags) == 0 {
		return nil
	}

	if current == nil {
		return &tags[0].Glyph
	}

	for i, tag := range tags {
		if tag.Glyph == *current && i+1 < len(tags) {
			return &tags[i+1].Glyph
		}
	}

	return nil
}

func nextSelectionTag(current []*string, tags []config.Tag) *string {
	if len(tags) == 0 || len(current) == 0 {
		return nil
	}

	for _, tag := range current[1:] {
		if !isSameTag(current[0], tag) {
			return &tags[0].Glyph
		}
	}

	return nextTag(current[0], tags)
}

func selectionTags(sel selection, rows []agentRow) []*string {
	agents := chosenAgents(sel, rows)

	return xslice.Map(agents, func(agent agentstore.Agent) *string { return agent.Tag })
}

func isSameTag(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	return *a == *b
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

const settingUpPlaceholder = "_setting up…_\n\nRunning the setup action. This agent has no pane yet."

// failureReasonKey is the metadata key a failed create or cleanup records its reason under.
// Excluded from the generic metadata dump in agentFields, since failedPlaceholder is
// responsible for surfacing it — never both at once.
const failureReasonKey = "error"

func (m model) failedPlaceholder(agent agentstore.Agent) string {
	label, kind := "setup", config.ActionLogSetup
	if agent.State == agentstore.StateFailedToCleanup {
		label, kind = "teardown", config.ActionLogTeardown
	}

	if m.hasActionLog(agent.ID, kind) {
		return fmt.Sprintf("_%s failed_\n\nPress \"l\" to see the %s log.", label, label)
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
	below := m.detailsView() + "\n" + m.renderHelp(m.agentHelp())

	all := m.agentRows()
	rows := make([]string, 0, len(all))
	for i, r := range all {
		hovered := i == m.tree.cursor
		prefix := "  "
		if hovered {
			prefix = m.rowCursorStyle().Render("› ")
		}
		prefix = selectionGutter(!r.header && m.tree.selected.has(r.item.agent.ID)) + prefix

		if r.header {
			label := m.filter.highlight(r.workflow.Name, m.nameStyle(hovered, false))
			rows = append(rows, prefix+dimStyle.Render(workflowIcon)+" "+label+hiddenTally(r.hiddenCount, m.tree.showHidden))
			continue
		}

		item := r.item
		state := item.agent.State
		if !item.alive && state != agentstore.StateSettingUp && !agentstore.IsFailed(state) {
			state = agentstore.StateIdle
		}

		name := m.filter.highlight(clipName(item.agent.Name), m.nameStyle(hovered, !item.alive))
		icon := lipgloss.NewStyle().Foreground(stateColor(state)).Render(stateIcon(state))
		rows = append(rows, prefix+"  "+icon+" "+name+" "+worktreeRemovedMark(item.agent)+scheduledMark(item.agent)+hiddenMark(item.agent.Hidden)+tagGlyph(item.agent, m.tags)+unreadMailMark(item.unreadMail))
	}

	return m.listView("Agents and Workflows", rows, m.tree.cursor, below)
}

func (m model) openNewAgent(workflow *workflowstore.Workflow) (tea.Model, tea.Cmd) {
	if workflow == nil {
		m.report("Could not start an agent", errNoWorkflow)

		return m, nil
	}

	m.target.newAgentWorkflow = workflow
	prefill := agentFormValues{
		Harness:       workflow.Harness,
		Dir:           workflow.Cwd,
		Flags:         workflow.Flags,
		SetupAction:   inheritAction,
		CleanupAction: inheritAction,
	}
	m.form, m.values.agent = newAgentForm(m.keys.Form, prefill, m.getAvailableActions())
	m.screen = screenForm

	return m, m.form.Init()
}

func (m model) updateRename(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateSubmitForm(msg, model.submitRename)
}

func (m model) submitRename() (tea.Model, tea.Cmd) {
	if m.values.rename == nil {
		return m.closeForm()
	}

	target := m.target.renameAgent
	name := strings.TrimSpace(*m.values.rename)
	cmd := m.writeCmd(
		fmt.Sprintf("Could not rename agent %q", target.Name),
		func(a api) error { return a.renameAgent(target, name) },
	)
	return m, tea.Batch(cmd, m.holdForm())
}

func (m model) submitAgent() (tea.Model, tea.Cmd) {
	if m.target.newAgentWorkflow == nil || m.values.agent == nil {
		m.report("Could not start the agent", errNoWorkflow)

		return m.closeForm()
	}

	setupAction := actionOverride(m.values.agent.SetupAction)
	effectiveSetup := m.target.newAgentWorkflow.SetupAction
	if setupAction != nil {
		effectiveSetup = *setupAction
	}

	var pane string
	if effectiveSetup != "" && !m.setupPending {
		m = m.closeInfo()
		if m.claimInfo(infoSetup, "") {
			pane = m.frame.Pane(layout.RegionInfo)
			m.setupPending = true
		}
	}

	params := createAgentParams{
		Prompt:        m.values.agent.Prompt,
		Title:         m.values.agent.Title,
		Harness:       m.values.agent.Harness,
		Dir:           m.currentDir(m.values.agent.Dir),
		Flags:         m.values.agent.Flags,
		SetupAction:   setupAction,
		CleanupAction: actionOverride(m.values.agent.CleanupAction),
		WorkflowID:    m.target.newAgentWorkflow.ID,
		SetupPane:     pane,
	}

	cmd := m.createCmd(params)

	if pane == "" {
		return m, tea.Batch(cmd, m.holdForm())
	}

	next, closeCmd := m.closeForm()

	return next, tea.Batch(cmd, closeCmd)
}

func actionOverride(selected string) *string {
	if selected == inheritAction {
		return nil
	}

	return &selected
}

func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateSubmitForm(msg, model.submitAgent)
}
