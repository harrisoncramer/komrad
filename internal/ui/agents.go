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

// updateAgents handles the keys common to both agents-screen modes, then hands off to
// whichever mode the tree is currently in.
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

// updateAgentsBrowsing handles the agents screen when no rows are selected: screen-level
// keys, then whatever the cursor is hovering over, then keys that act on the hovered agent.
func (m model) updateAgentsBrowsing(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	if isBound(k, m.keys.Filter.Open) {
		return m.openFilter()
	}
	if m.filter.standing() && isBound(k, m.keys.Agents.Back) {
		return m.clearFilter()
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

// updateHoveredAgent handles keys that act on the agent under the cursor. It is a no-op
// when the cursor is on a workflow header rather than an agent.
func (m model) updateHoveredAgent(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	return m.withSelectedAgent(func(item agentItem) (tea.Model, tea.Cmd) {
		switch {
		case isBound(k, m.keys.Agents.Tag):
			return m.tagAgent(item.agent)
		case isBound(k, m.keys.Agents.Hide):
			return m.hideAgent(item.agent)
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

// withSelectedAgent runs fn against the agent under the cursor, or is a no-op when the
// cursor is on a workflow header or the list is empty.
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

// archiveAgentsConfirm opens the archive prompt for one or more agents. Nothing is
// archived until the prompt is answered yes.
func (m model) archiveAgentsConfirm(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.target.archiveAgents = agents
	m.form, m.values.archive = archiveAgentsForm(agents, m.keys.Form)
	m.screen = screenAgentArchive

	return m, m.form.Init()
}

// updateAgentArchive drives the archive prompt. The answer decides whether the agents are put
// away, put away and then destroyed, or left alone, and every outcome returns to the list.
func (m model) updateAgentArchive(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateConfirmForm(msg, confirmFormActions{
		write: func(m model) tea.Cmd {
			target := m.target.archiveAgents
			if len(target) == 0 {
				return nil
			}

			switch m.archiveAnswer() {
			case archiveChoiceArchive:
				_, write := m.archiveAgents(target)

				return write
			case archiveChoiceArchiveDelete:
				_, write := m.archiveAndDeleteAgents(target)

				return write
			case archiveChoiceCancel:
				return nil
			}

			return nil
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

func (m model) archiveAgents(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
	if len(agents) == 0 {
		return m, nil
	}

	m.releasePanes(agents)

	return m, m.writeCmd(
		fmt.Sprintf("Could not archive %s", describeAgents(agents)),
		func(a api) error { return a.archiveAgents(agentIDs(agents)) },
	)
}

// archiveAndDeleteAgents puts agents away and then destroys them, the two calls in one write
// so a refused archive leaves the records alone rather than destroying what was never stopped.
func (m model) archiveAndDeleteAgents(agents []agentstore.Agent) (tea.Model, tea.Cmd) {
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

// A staged pane must be released before the service kills it.
func (m model) releasePanes(agents []agentstore.Agent) {
	for _, agent := range agents {
		m.frame.ReleaseGuest(agent.Pane)
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
		if item.alive {
			return m, nil
		}

		return m, m.resumeCmd(resumeParams{
			summary: fmt.Sprintf("Could not resume agent %q", item.agent.Name),
			id:      item.agent.ID,
		})
	})
}

// Preserve the selected row when hidden rows change the list indexes.
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

// agentsDetails is what the row under the agents cursor holds: a workflow header carries the
// defaults its agents inherit, and an agent carries what it was launched with plus whatever
// it has since recorded about its own work.
func (m model) agentsDetails() details {
	r, ok := rowAt(m.agentRows(), m.tree.cursor)
	if !ok {
		return details{}
	}

	if r.header {
		return details{
			id:     r.workflow.ID,
			title:  r.workflow.Name,
			fields: workflowFields(r.workflow, m.agentCount(r.workflow.ID)),
		}
	}

	return details{
		id:     r.item.agent.ID,
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

	// The fields of whatever is hovered are drawn in the sidebar, so the stage is left for
	// the text that has nowhere else to go: an agent's own pane, and the prose a workflow
	// carries for the agents it starts.
	switch {
	case r.header:
		m.stageDocument(workflowContextMarkdown(r.workflow))
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

// Mixed selections collapse to the first tag so their next state is deterministic.
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

// dormantPlaceholder is what the stage holds for an agent with nothing running: the fields
// are in the sidebar, so this says only why the pane is empty and what brings it back.
const dormantPlaceholder = "_dormant — not running_\n\nPress open to resume."

func (m model) agentsView() string {
	// The details and the legend are both chrome under the list, handed over together so the
	// list's own height is measured against everything below it.
	below := m.detailsView() + "\n" + m.renderHelp(m.agentHelp())

	all := m.agentRows()
	rows := make([]string, 0, len(all))
	for i, r := range all {
		hovered := i == m.tree.cursor
		prefix := "  "
		if hovered {
			prefix = cursorStyle.Render("› ")
		}
		prefix = selectionGutter(!r.header && m.tree.selected.has(r.item.agent.ID)) + prefix

		if r.header {
			label := m.filter.highlight(r.workflow.Name, nameStyle(hovered, false))
			rows = append(rows, prefix+dimStyle.Render(workflowIcon)+" "+label+hiddenTally(r.hiddenCount, m.tree.showHidden))
			continue
		}

		item := r.item
		state := item.agent.State
		if !item.alive {
			state = agentstore.StateIdle
		}

		name := m.filter.highlight(clipName(item.agent.Name), nameStyle(hovered, !item.alive))
		icon := lipgloss.NewStyle().Foreground(stateColor(state)).Render(stateIcon(state))
		rows = append(rows, prefix+"  "+icon+" "+name+scheduledMark(item.agent)+hiddenMark(item.agent.Hidden)+tagGlyph(item.agent, m.tags)+unreadMailMark(item.unreadMail))
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

	params := createAgentParams{
		Prompt:        m.values.agent.Prompt,
		Title:         m.values.agent.Title,
		Harness:       m.values.agent.Harness,
		Dir:           m.currentDir(m.values.agent.Dir),
		Flags:         m.values.agent.Flags,
		SetupAction:   actionOverride(m.values.agent.SetupAction),
		CleanupAction: actionOverride(m.values.agent.CleanupAction),
		WorkflowID:    m.target.newAgentWorkflow.ID,
	}

	cmd := m.createCmd(params)

	return m, tea.Batch(cmd, m.holdForm())
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
