package ui

import (
	"context"
	"fmt"
	"strings"

	"claude-agents/internal/appconfig"
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
	// hiddenCount is how many of a header's agents are hidden, so the folder can say
	// how much of itself it is not showing. It is always the full count, whether or
	// not the hidden ones are on screen.
	hiddenCount int
}

// agentRows arranges the agents into the displayed tree. Every workflow is a
// header followed by its agents, and an empty workflow still gets a header so a
// new one can be hovered to add its first agent.
//
// Every agent belongs to a workflow, so there is no trailing group for ones that
// do not. An agent whose workflow is not in the listing, which means the
// workflow was deleted, is left out rather than shown under a header that is not
// there.
//
// A hidden agent gives up its row and is counted against its workflow's header
// instead, unless the panel is showing the hidden ones. The filtering happens here
// rather than at the listing, so every other screen and every count still sees the
// whole set.
func (m model) agentRows() []agentRow {
	byWorkflow := make(map[string][]agentItem)
	hiddenCounts := make(map[string]int)
	for _, it := range m.agents {
		if it.agent.Hidden {
			hiddenCounts[it.agent.WorkflowID]++
			if !m.showHidden {
				continue
			}
		}

		byWorkflow[it.agent.WorkflowID] = append(byWorkflow[it.agent.WorkflowID], it)
	}

	var rows []agentRow
	for wi, w := range m.workflows {
		rows = append(rows, agentRow{header: true, label: w.Name, workflowIndex: wi, hiddenCount: hiddenCounts[w.ID]})
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

	// The trash is the same: a screen of its own, holding what this tree no longer
	// shows, so the key that opens it cannot belong to a row.
	if bound(k, m.keys.Trash.Open) {
		return m.enterTrash()
	}

	// Whether the hidden agents are drawn is a property of the whole list rather than
	// of the row under the cursor, so it works from a workflow header as much as from
	// an agent.
	if bound(k, m.keys.Agents.ShowHidden) {
		return m.toggleHiddenAgents()
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
	case bound(k, m.keys.Agents.Mark):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		return m.markAgent(item.agent)
	case bound(k, m.keys.Agents.Hide):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		return m.hideAgent(item.agent)
	case bound(k, m.keys.Agents.Rename):
		item, ok := m.selectedAgent()
		if !ok {
			return m, nil
		}
		m.renameTarget = item.agent
		m.form, m.renameValue = renameForm(item.agent.Name, m.keys.Form)
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

		return m, m.writeCmd(
			fmt.Sprintf("Could not delete agent %q", item.agent.Name),
			func(a api) error { return a.archiveAgent(item.agent) },
		)
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

	return m, m.restoreCmd(
		fmt.Sprintf("Could not restore agent %q", item.agent.Name),
		item.agent.ID,
		screenAgents,
	)
}

// archiveAgent stops an agent and hides it. The record and transcript stay, so
// search still reaches it.
func (a api) archiveAgent(agent agentstore.Agent) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := a.agents.ArchiveAgent(ctx, connect.NewRequest(&agentv1.ArchiveAgentRequest{Name: agent.Name}))

	return err
}

// toggleHiddenAgents draws the hidden agents alongside the rest, or puts them away
// again. Rows appear and vanish above the cursor either way, so it follows the agent
// it was on rather than the index it held, the same way a refresh does.
func (m model) toggleHiddenAgents() (tea.Model, tea.Cmd) {
	selected := ""
	if item, ok := m.selectedAgent(); ok {
		selected = item.agent.Name
	}

	m.showHidden = !m.showHidden

	rows := m.agentRows()
	if idx := rowIndexOfAgentName(rows, selected); idx >= 0 {
		m.agentCursor = idx
	} else {
		m.agentCursor = clampCursor(m.agentCursor, len(rows))
	}
	m.stageSelection()

	return m, nil
}

// markAgent steps the hovered agent to the next mark.
func (m model) markAgent(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	next := nextMark(agent.Mark, m.marks)

	return m, m.writeCmd(
		fmt.Sprintf("Could not mark agent %q", agent.Name),
		func(a api) error { return a.setMark(agent, next) },
	)
}

// hideAgent takes the hovered agent out of the list, or puts a revealed one back.
// One key does both directions, so which way it went has to come from the row for
// the failure to name it.
func (m model) hideAgent(agent agentstore.Agent) (tea.Model, tea.Cmd) {
	failure := fmt.Sprintf("Could not hide agent %q", agent.Name)
	if agent.Hidden {
		failure = fmt.Sprintf("Could not unhide agent %q", agent.Name)
	}

	return m, m.writeCmd(failure, func(a api) error { return a.setHidden(agent, !agent.Hidden) })
}

// setHidden takes an agent out of the list or puts it back. Nothing about the agent
// changes: it keeps running and every other view still holds it.
func (a api) setHidden(agent agentstore.Agent, hidden bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := a.agents.SetHidden(ctx, connect.NewRequest(&agentv1.SetHiddenRequest{Id: agent.ID, Hidden: hidden}))

	return err
}

// setMark puts a mark on an agent, and a nil mark clears the one it carries.
func (a api) setMark(agent agentstore.Agent, mark *string) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := a.agents.SetMark(ctx, connect.NewRequest(&agentv1.SetMarkRequest{Id: agent.ID, Mark: mark}))

	return err
}

// restoreAgent brings an agent back and returns it as the service now holds it.
func (a api) restoreAgent(id string) (*agentv1.Agent, error) {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	resp, err := a.agents.RestoreAgent(ctx, connect.NewRequest(&agentv1.RestoreAgentRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to restore agent %q: %w", id, err)
	}

	return resp.Msg.GetAgent(), nil
}

// stageSelection updates the stage pane to reflect what the cursor is on. A live
// agent's pane is swapped in, a dormant agent and a workflow header show rendered
// markdown, and anything else blanks the stage. With info pinned, an agent shows
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

// hiddenIcon is a dotted circle, single-width for the same reason the clock is.
const hiddenIcon = "◌"

// hiddenMark is drawn after a hidden row's name, which is only on screen at all while
// the hidden ones are being shown. Without it a revealed row is indistinguishable
// from the rest and the hide key reads as doing nothing. Agents and schedules both
// draw it, since hiding means the same thing on either screen.
func hiddenMark(hidden bool) string {
	if !hidden {
		return ""
	}

	return " " + dimStyle.Render(hiddenIcon)
}

// hiddenTally is the count of hidden agents drawn after a workflow's name, so a
// folder says how much of itself it is not showing. It is dropped once they are on
// screen, where they are rows of their own and the count would only say it twice.
func hiddenTally(count int, showing bool) string {
	if count == 0 || showing {
		return ""
	}

	return " " + dimStyle.Render(fmt.Sprintf("+%d", count))
}

// markGlyph is the glyph the user marked an agent with, drawn after the row rather
// than in the state column so it reads as their own mark and not another lifecycle
// state. It takes the colour that mark carries in the config.
func markGlyph(agent agentstore.Agent, marks []appconfig.Mark) string {
	if agent.Mark == nil {
		return ""
	}

	return " " + lipgloss.NewStyle().Foreground(markColor(*agent.Mark, marks)).Render(*agent.Mark)
}

// markColor is the colour a glyph is drawn in. One the config does not list, or one
// listed with no colour of its own, falls back rather than going undrawn.
func markColor(glyph string, marks []appconfig.Mark) lipgloss.Color {
	for _, mark := range marks {
		if mark.Glyph == glyph && mark.Color != "" {
			return lipgloss.Color(mark.Color)
		}
	}

	return markedColor
}

// nextMark is where the mark key moves an agent, stepping through the configured
// marks and past the last one back to unmarked. A mark that is not configured clears
// for the same reason it draws in the fallback colour, so the key is always a way
// back out of one.
func nextMark(current *string, marks []appconfig.Mark) *string {
	if len(marks) == 0 {
		return nil
	}

	if current == nil {
		return &marks[0].Glyph
	}

	for i, mark := range marks {
		if mark.Glyph == *current && i+1 < len(marks) {
			return &marks[i+1].Glyph
		}
	}

	return nil
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
	if !alive {
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
	if agent.Mark != nil {
		fmt.Fprintf(&b, "- mark: %s\n", *agent.Mark)
	}
	if agent.Hidden {
		b.WriteString("- hidden from the list\n")
	}
	// What the agent recorded about its own work, which is the one part of this record
	// it wrote rather than took at launch. The values go in bare: they are mostly URLs,
	// terminals linkify those on their own, and markdown link syntax would hide the
	// target this section exists to show. An agent that recorded nothing draws no
	// heading at all rather than an empty one.
	if len(agent.Metadata) > 0 {
		b.WriteString("\n## metadata\n\n")
		for _, entry := range agent.Metadata {
			fmt.Fprintf(&b, "- %s: %s\n", entry.Key, entry.Value)
		}
	}
	if !alive {
		b.WriteString("\nPress open to restore.\n")
	}

	return b.String()
}

// actionHints renders the key bindings one per line with an aligned key column.
func (m model) actionHints() string {
	// The key that draws the hidden agents toggles, so the legend names the direction
	// it would go rather than the action in the abstract.
	showHiddenDesc := "show hidden"
	if m.showHidden {
		showHiddenDesc = "put hidden away"
	}

	hints := append(m.rowHints(), m.screenHints()...)
	hints = append(hints,
		hint{hintKeys(m.keys.Agents.ShowHidden), showHiddenDesc},
		hint{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		hint{hintKeys(m.keys.Agents.Back), "back"},
	)

	return renderHints(hints)
}

// rowHints are the bindings that act on the row under the cursor, which is a
// workflow header or an agent. The info, mark, and hide keys all toggle, so their
// lines name what that row would do rather than the action in the abstract.
func (m model) rowHints() []hint {
	if _, hoveringWorkflow := m.hoveredWorkflow(); hoveringWorkflow {
		return []hint{
			{hintKeys(m.keys.Agents.New), "new agent"},
			{hintKeys(m.keys.Workflows.New), "new workflow"},
			{hintKeys(m.keys.Workflows.Rename), "rename"},
			{hintKeys(m.keys.Workflows.Edit), "edit"},
			{hintKeys(m.keys.Workflows.Delete), "delete"},
		}
	}

	infoDesc := "info"
	if m.infoPinned {
		infoDesc = "hide info"
	}

	markDesc, hideDesc := "mark", "hide"
	if item, ok := m.selectedAgent(); ok {
		if next := nextMark(item.agent.Mark, m.marks); next != nil {
			markDesc = "mark " + *next
		} else {
			markDesc = "unmark"
		}
		if item.agent.Hidden {
			hideDesc = "unhide"
		}
	}

	return []hint{
		{hintKeys(m.keys.Agents.Open), "open"},
		{hintKeys(m.keys.Agents.Info), infoDesc},
		{hintKeys(m.keys.Agents.Mark), markDesc},
		{hintKeys(m.keys.Agents.Hide), hideDesc},
		{hintKeys(m.keys.Agents.New), "new agent"},
		{hintKeys(m.keys.Workflows.New), "new workflow"},
		{hintKeys(m.keys.Agents.Rename), "rename"},
		{hintKeys(m.keys.Agents.Delete), "delete"},
	}
}

// screenHints are the screens reachable from anywhere in the tree. They are
// appended to whichever legend is showing, since none of them act on the row
// under the cursor and all of them work from a workflow header as much as from
// an agent.
func (m model) screenHints() []hint {
	return []hint{
		{hintKeys(m.keys.Agents.Search), "search"},
		{hintKeys(m.keys.Schedules.Open), "schedules"},
		{hintKeys(m.keys.Trash.Open), "trash"},
	}
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
	header := headerStyle.Render("Agents and Workflows") + "\n\n"
	hints := "\n" + m.actionHints()

	// The list is a tree. Each workflow is a folder header with its agents nested
	// beneath it, so an agent's parent shows by position rather than a per-row
	// marker.
	all := m.agentRows()
	rows := make([]string, 0, len(all))
	for i, r := range all {
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
			rows = append(rows, prefix+dimStyle.Render("⧉")+" "+label+hiddenTally(r.hiddenCount, m.showHidden))
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
		rows = append(rows, prefix+"  "+icon+" "+name+scheduledMark(item.agent)+hiddenMark(item.agent.Hidden)+markGlyph(item.agent, m.marks))
	}

	return header + renderList(rows, m.agentCursor, m.listLines(header, hints)) + hints
}
