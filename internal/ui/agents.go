package ui

import (
	"context"
	"fmt"
	"strings"

	agentv1 "claude-agents/proto/gen/agent/v1"

	"claude-agents/lib/dates"

	"connectrpc.com/connect"

	agentstore "claude-agents/internal/store/agent"
	workflowstore "claude-agents/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// agentRow is one line of the agents list, either a workflow header or an agent
// nested beneath one. The cursor moves over both but actions apply only to agent
// rows.
type agentRow struct {
	header bool
	label  string    // the workflow's name when header is true
	item   agentItem // the agent when header is false
	// workflowIndex is the header's index in m.workflows so workflow actions can
	// target it, and the index of the enclosing workflow for an agent row, so a new
	// agent started from one lands in the same place.
	workflowIndex int
}

// agentRows arranges the agents into the displayed tree. Every workflow is a
// header followed by its agents, and an empty workflow still gets a header so a
// new one can be hovered to add its first agent.
//
// Every agent belongs to a workflow, so there is no trailing group for ones that
// do not. An agent whose workflow is not in the listing, which means the
// workflow was deleted, is left out rather than shown under a header that is not
// there.
func (m model) agentRows() []agentRow {
	byWorkflow := make(map[string][]agentItem)
	for _, it := range m.agents {
		byWorkflow[it.agent.WorkflowID] = append(byWorkflow[it.agent.WorkflowID], it)
	}

	var rows []agentRow
	for wi, w := range m.workflows {
		rows = append(rows, agentRow{header: true, label: w.Name, workflowIndex: wi})
		for _, it := range byWorkflow[w.ID] {
			rows = append(rows, agentRow{item: it, workflowIndex: wi})
		}
	}

	return rows
}

// selectedAgent returns the agent under the cursor, or false when the cursor is on
// a workflow header or the list is empty.
func (m model) selectedAgent() (agentItem, bool) {
	rows := m.agentRows()
	if m.agentCursor < 0 || m.agentCursor >= len(rows) || rows[m.agentCursor].header {
		return agentItem{}, false
	}

	return rows[m.agentCursor].item, true
}

// hoveredWorkflow returns the m.workflows index of the workflow header under the
// cursor and true, or false when the cursor is not on a real workflow header.
func (m model) hoveredWorkflow() (int, bool) {
	rows := m.agentRows()
	if m.agentCursor < 0 || m.agentCursor >= len(rows) {
		return 0, false
	}

	r := rows[m.agentCursor]
	if !r.header {
		return 0, false
	}

	return r.workflowIndex, true
}

// hoveredWorkflowRecord is the workflow the cursor is inside, whether it sits on
// the header or on one of the agents under it, so a new agent started from
// anywhere in the tree lands in the workflow it was started from. It is nil when
// the tree is empty, which means no workflow exists to start one in.
func (m model) hoveredWorkflowRecord() *workflowstore.Workflow {
	rows := m.agentRows()
	if m.agentCursor < 0 || m.agentCursor >= len(rows) {
		return nil
	}

	idx := rows[m.agentCursor].workflowIndex
	if idx < 0 || idx >= len(m.workflows) {
		return nil
	}

	workflow := m.workflows[idx]

	return &workflow
}

// firstAgentRowIndex returns the index of the first agent row, skipping a leading
// header so entering the list lands on an agent rather than a folder label.
func firstAgentRowIndex(rows []agentRow) int {
	for i, r := range rows {
		if !r.header {
			return i
		}
	}

	return 0
}

// rowIndexOfAgentName returns the row of the named agent, or -1 when it is gone, so
// a refresh can keep the cursor on the same agent as the tree is rebuilt.
func rowIndexOfAgentName(rows []agentRow, name string) int {
	for i, r := range rows {
		if !r.header && r.item.agent.Name == name {
			return i
		}
	}

	return -1
}

// updateAgents drives the nav list. The nav keys move the selection and restage,
// and letter keys act on the hovered agent or workflow.
func (m model) updateAgents(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// New workflow is global to the agents view regardless of cursor position, so it
	// never collides with the per-row new-agent key.
	if bound(k, m.keys.Workflows.New) {
		return m.newWorkflowForm()
	}

	// Search is likewise global to the agents view. It opens over the whole corpus,
	// not the hovered row.
	if bound(k, m.keys.Agents.Search) {
		return m.enterSearch()
	}

	// So is the schedules screen, which is a view of its own rather than a row in
	// this tree.
	if bound(k, m.keys.Schedules.Open) {
		return m.enterSchedules()
	}

	// On a workflow header the workflow bindings act on that folder's workflow.
	if wfIdx, ok := m.hoveredWorkflow(); ok {
		return m.updateHoveredWorkflow(key, wfIdx)
	}

	switch {
	case bound(k, m.keys.Agents.Quit):
		return m, m.hide()
	case bound(k, m.keys.Agents.Back):
		return m, m.hide()
	case bound(k, m.keys.Agents.New):
		return m.openNewAgent(m.hoveredWorkflowRecord())
	case bound(k, m.keys.Nav.Up):
		if m.agentCursor > 0 {
			m.agentCursor--
			m.infoPinned = false
			m.stageSelection()
		}
	case bound(k, m.keys.Nav.Down):
		if m.agentCursor < len(m.agentRows())-1 {
			m.agentCursor++
			m.infoPinned = false
			m.stageSelection()
		}
	case bound(k, m.keys.Agents.Info):
		if _, ok := m.selectedAgent(); !ok {
			return m, nil
		}
		m.infoPinned = !m.infoPinned
		m.stageSelection()
	case bound(k, m.keys.Agents.Open):
		return m.openAgent()
	case bound(k, m.keys.Agents.Rename):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		m.renameTarget = item.agent
		m.form = renameForm(item.agent.Name, m.keys.Form)
		m.screen = screenRename
		return m, m.form.Init()
	case bound(k, m.keys.Agents.Delete):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		// Killing a pane while it sits in the stage slot would take the stage down
		// with it, so it is swapped back out first.
		m.stage.Release(item.agent.Pane)
		m.report(fmt.Sprintf("Could not delete agent %q", item.agent.Name), m.archiveAgent(item.agent))
		m.reload()
		m.agentCursor = clampCursor(m.agentCursor, len(m.agentRows()))
		m.stageSelection()
	}

	return m, nil
}

// openAgent acts on the hovered agent. A dormant agent is restored and staged
// live. A live agent is already staged from hovering, so opening it only unpins the
// info view to bring the agent back. Moving focus onto it is a plain tmux pane move
// the user drives.
func (m model) openAgent() (tea.Model, tea.Cmd) {
	item, ok := m.selectedAgent()
	if !ok {
		return m, nil
	}
	if item.alive {
		if m.infoPinned {
			m.infoPinned = false
			m.stageSelection()
		}
		return m, nil
	}

	restored, err := m.restoreAgent(item.agent.ID)
	m.report(fmt.Sprintf("Could not restore agent %q", item.agent.Name), err)
	if err != nil {
		return m, nil
	}
	m.infoPinned = false
	m.reload()
	// Restoring may rename an agent whose name was taken while it sat archived, so
	// the cursor follows the name that came back rather than the one it had.
	m.agentCursor = clampCursor(rowIndexOfAgentName(m.agentRows(), restored.GetName()), len(m.agentRows()))
	m.stage.Agent(restored.GetPaneId())

	return m, nil
}

// archiveAgent stops an agent and hides it. The record and transcript stay, so
// search still reaches it.
func (m model) archiveAgent(agent agentstore.Agent) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := m.api.agents.ArchiveAgent(ctx, connect.NewRequest(&agentv1.ArchiveAgentRequest{Name: agent.Name}))

	return err
}

// restoreAgent brings an agent back and returns it as the service now holds it.
func (m model) restoreAgent(id string) (*agentv1.Agent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	resp, err := m.api.agents.RestoreAgent(ctx, connect.NewRequest(&agentv1.RestoreAgentRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to restore agent %q: %w", id, err)
	}

	return resp.Msg.GetAgent(), nil
}

// stageSelection updates the stage pane to reflect what the cursor is on. A live
// agent's pane is swapped in, a dormant agent and a workflow header show markdown
// through bat, and anything else blanks the stage. With info pinned, an agent shows
// its stored record instead of itself.
func (m model) stageSelection() {
	rows := m.agentRows()
	if m.agentCursor < 0 || m.agentCursor >= len(rows) {
		m.stage.Blank()
		return
	}

	r := rows[m.agentCursor]
	switch {
	case r.header && r.workflowIndex >= 0:
		m.stage.Text(workflowMarkdown(m.workflows[r.workflowIndex], m.agentCount(m.workflows[r.workflowIndex].ID)))
	case r.header:
		m.stage.Blank()
	case m.infoPinned:
		m.stage.Text(agentMarkdown(r.item.agent, r.item.alive, m.scheduleName(r.item.agent)))
	case r.item.alive:
		m.stage.Agent(r.item.agent.Pane)
	default:
		m.stage.Text(agentMarkdown(r.item.agent, false, m.scheduleName(r.item.agent)))
	}
}

// agentCount returns how many listed agents belong to the given workflow id.
func (m model) agentCount(workflowID string) int {
	n := 0
	for _, it := range m.agents {
		if it.agent.WorkflowID == workflowID {
			n++
		}
	}

	return n
}

// scheduledIcon is a clock face rather than an emoji one, which would render
// double-width in some terminals and push the column out of line.
const scheduledIcon = "◷"

// scheduledMark is the clock rendered after a scheduled agent's name. One a person
// launched carries nothing, since that is the ordinary case and a marker on every
// row would say nothing.
func scheduledMark(agent agentstore.Agent) string {
	if agent.ScheduleID == nil {
		return ""
	}

	return " " + dimStyle.Render(scheduledIcon)
}

// scheduleName is the display name of the schedule that launched an agent, or
// empty for one a person started. It reads from the list the panel already
// polls, so a schedule deleted since the agent ran resolves to nothing rather
// than to a stale name.
func (m model) scheduleName(agent agentstore.Agent) string {
	if agent.ScheduleID == nil {
		return ""
	}

	for _, item := range m.schedules {
		if item.schedule.ID == *agent.ScheduleID {
			return item.schedule.Name
		}
	}

	return ""
}

func orDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}

	return v
}

// workflowMarkdown renders a workflow's name, shared context, and config for the
// stage.
func workflowMarkdown(w workflowstore.Workflow, agents int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", w.Name)
	if strings.TrimSpace(w.Context) != "" {
		b.WriteString(w.Context + "\n\n")
	} else {
		b.WriteString("_no shared context set_\n\n")
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "- agents: %d\n", agents)
	fmt.Fprintf(&b, "- directory: %s\n", orDash(w.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(w.Flags))

	return b.String()
}

// agentMarkdown renders an agent's stored record for the stage. A dormant agent has
// no live pane to show, so this is all there is of it. A live one shows the same
// record on demand, since its running pane occupies the stage and hides the cwd and
// flags it was launched with. The values are the record as stored at launch rather
// than what the running agent has done since.
func agentMarkdown(agent agentstore.Agent, alive bool, schedule string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", agent.Name)
	if alive {
		fmt.Fprintf(&b, "_running in %s_\n\n", orDash(agent.Pane))
	} else {
		b.WriteString("_dormant — not running_\n\n")
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "- directory: %s\n", orDash(agent.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(agent.Flags))
	fmt.Fprintf(&b, "- resume: %s\n", orDash(agent.ClaudeSessionID))
	if schedule != "" {
		fmt.Fprintf(&b, "- launched by: %s\n", schedule)
	}
	if created := dates.Friendly(agent.CreatedAt); created != "" {
		fmt.Fprintf(&b, "- created: %s\n", created)
	}
	if !alive {
		b.WriteString("\nPress open to restore.\n")
	}

	return b.String()
}

// actionHints renders the key bindings one per line with an aligned key column.
func (m model) actionHints() string {
	infoDesc := "info"
	if m.infoPinned {
		infoDesc = "hide info"
	}

	hints := []hint{
		{hintKeys(m.keys.Agents.Open), "open"},
		{hintKeys(m.keys.Agents.Info), infoDesc},
		{hintKeys(m.keys.Agents.New), "new agent"},
		{hintKeys(m.keys.Workflows.New), "new workflow"},
		{hintKeys(m.keys.Agents.Rename), "rename"},
		{hintKeys(m.keys.Agents.Delete), "delete"},
		{hintKeys(m.keys.Agents.Search), "search"},
		{hintKeys(m.keys.Schedules.Open), "schedules"},
		{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		{hintKeys(m.keys.Agents.Back), "back"},
	}
	if _, hoveringWorkflow := m.hoveredWorkflow(); hoveringWorkflow {
		hints = []hint{
			{hintKeys(m.keys.Agents.New), "new agent"},
			{hintKeys(m.keys.Workflows.New), "new workflow"},
			{hintKeys(m.keys.Workflows.Rename), "rename"},
			{hintKeys(m.keys.Workflows.Edit), "edit"},
			{hintKeys(m.keys.Workflows.Delete), "delete"},
			{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		}
	}

	return renderHints(hints)
}

// hint is one line of the key legend under a list, a binding and what it does.
type hint struct{ key, desc string }

// renderHints lays the legend out as two columns, the keys padded to the widest
// so the descriptions line up.
func renderHints(hints []hint) string {
	keyWidth := 0
	for _, h := range hints {
		if w := lipgloss.Width(h.key); w > keyWidth {
			keyWidth = w
		}
	}
	keyStyle := lipgloss.NewStyle().Width(keyWidth + 1)

	var b strings.Builder
	for _, h := range hints {
		b.WriteString(dimStyle.Render(keyStyle.Render(h.key) + h.desc))
		b.WriteString("\n")
	}

	return b.String()
}

// agentsView renders the nav list, the panel's own pane. The hovered resource is
// shown in the separate stage pane, not here.
func (m model) agentsView() string {
	var list strings.Builder
	list.WriteString(headerStyle.Render("Agents and Workflows") + "\n\n")

	// The list is a tree. Each workflow is a folder header with its agents nested
	// beneath it, so an agent's parent shows by position rather than a per-row
	// marker.
	for i, r := range m.agentRows() {
		selected := i == m.agentCursor
		prefix := "  "
		if selected {
			prefix = cursorStyle.Render("› ")
		}

		if r.header {
			label := r.label
			if selected {
				label = cursorStyle.Render(label)
			}
			list.WriteString(prefix + dimStyle.Render("⧉") + " " + label + "\n")
			continue
		}

		item := r.item
		name := item.agent.Name
		if selected {
			name = cursorStyle.Render(name)
		}

		state := item.agent.State
		if !item.alive {
			state = "" // dormant, neutral icon and color
			name = dimStyle.Render(item.agent.Name)
			if selected {
				name = cursorStyle.Render(item.agent.Name)
			}
		}

		icon := lipgloss.NewStyle().Foreground(stateColor(state)).Render(stateIcon(state))
		list.WriteString(prefix + "  " + icon + " " + name + scheduledMark(item.agent) + "\n")
	}

	list.WriteString("\n" + m.actionHints())

	return list.String()
}
