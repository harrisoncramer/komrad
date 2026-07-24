package panel

import (
	"strings"

	"claude-agents/internal/store"
	"claude-agents/lib/dates"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const chatListWidth = 32

// detailsHeight is how many text lines the hovered-chat details footer occupies,
// below the picker and preview. The preview is shrunk by this plus its border so
// the whole view still fits the popup.
const detailsHeight = 5

// orNA returns the value, or a dimmed "N/A" when it is blank. Every metadata
// view uses it so all show the same fields and an irrelevant one reads as N/A
// rather than vanishing, which would jitter the layout as the selection moves.
func orNA(v string) string {
	if strings.TrimSpace(v) == "" {
		return dimStyle.Render("N/A")
	}

	return v
}

// workflowName resolves a workflow id to its display name from the loaded
// workflow list, joining the two resources at display time rather than storing a
// name on the chat. It returns empty when the id is unknown.
func (m model) workflowName(id string) string {
	for _, w := range m.workflows {
		if w.ID == id {
			return w.Name
		}
	}

	return ""
}

// noWorkflowLabel heads the trailing group of chats that belong to no workflow.
const noWorkflowLabel = "No Workflow"

// chatRow is one line of the chats list: either a workflow header (a folder) or
// a chat nested under one. The cursor moves over both, but actions apply only to
// chat rows, so a header is a passive label.
type chatRow struct {
	header bool
	label  string   // workflow name, or noWorkflowLabel, when header is true
	item   chatItem // the chat, when header is false
	// workflowIndex points a workflow header at its entry in m.workflows, so the
	// workflow actions can target it. It is -1 for the no-workflow header and
	// unused for chat rows.
	workflowIndex int
}

// chatRows arranges the chats into the displayed tree. Every workflow is a
// header (shown even when empty, so a new one can be hovered to add its first
// chat) followed by its chats, then a trailing group of chats that belong to no
// workflow.
func (m model) chatRows() []chatRow {
	byWorkflow := make(map[string][]chatItem)
	var unassigned []chatItem
	for _, it := range m.chats {
		if it.agent.WorkflowID != nil {
			id := *it.agent.WorkflowID
			byWorkflow[id] = append(byWorkflow[id], it)
		} else {
			unassigned = append(unassigned, it)
		}
	}

	var rows []chatRow
	for wi, w := range m.workflows {
		chats := byWorkflow[w.ID]
		delete(byWorkflow, w.ID)
		rows = append(rows, chatRow{header: true, label: w.Name, workflowIndex: wi})
		for _, it := range chats {
			rows = append(rows, chatRow{item: it})
		}
	}

	// Chats whose workflow is unknown (e.g. it was just deleted) fall in with the
	// unassigned group rather than vanishing from the list.
	for _, leftover := range byWorkflow {
		unassigned = append(unassigned, leftover...)
	}
	rows = append(rows, chatRow{header: true, label: noWorkflowLabel, workflowIndex: -1})
	for _, it := range unassigned {
		rows = append(rows, chatRow{item: it})
	}

	return rows
}

// selectedChat returns the chat under the cursor and true, or false when the
// cursor is on a workflow header or the list is empty, so actions no-op there.
func (m model) selectedChat() (chatItem, bool) {
	rows := m.chatRows()
	if m.chatCursor < 0 || m.chatCursor >= len(rows) || rows[m.chatCursor].header {
		return chatItem{}, false
	}

	return rows[m.chatCursor].item, true
}

// hoveredWorkflow returns the m.workflows index of the workflow header under the
// cursor and true, or false when the cursor is not on a real workflow header
// (a chat, the no-workflow group, or an empty list).
func (m model) hoveredWorkflow() (int, bool) {
	rows := m.chatRows()
	if m.chatCursor < 0 || m.chatCursor >= len(rows) {
		return 0, false
	}

	r := rows[m.chatCursor]
	if !r.header || r.workflowIndex < 0 {
		return 0, false
	}

	return r.workflowIndex, true
}

// onNoWorkflowHeader reports whether the cursor sits on the no-workflow header,
// which has no workflow record behind it and so cannot be renamed or deleted.
func (m model) onNoWorkflowHeader() bool {
	rows := m.chatRows()
	if m.chatCursor < 0 || m.chatCursor >= len(rows) {
		return false
	}

	r := rows[m.chatCursor]
	return r.header && r.workflowIndex < 0
}

// firstChatRowIndex returns the index of the first chat row, skipping a leading
// header, so entering the list lands on a chat rather than a folder label.
func firstChatRowIndex(rows []chatRow) int {
	for i, r := range rows {
		if !r.header {
			return i
		}
	}

	return 0
}

// rowIndexOfChatName finds the row of the named chat, or -1 when it is gone, so a
// refresh can keep the cursor on the same chat as the tree is rebuilt.
func rowIndexOfChatName(rows []chatRow, name string) int {
	for i, r := range rows {
		if !r.header && r.item.agent.Name == name {
			return i
		}
	}

	return -1
}

// updateChats drives the split chats view: arrows move the selection and letter
// keys act on the hovered chat.
func (m model) updateChats(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// New workflow is global to the chats view, regardless of what the cursor is
	// on, so it never collides with the per-row new-chat key.
	if bound(k, m.keys.Workflows.New) {
		return m.newWorkflowForm()
	}

	// Hovering a workflow header lets the workflow bindings act on that folder's
	// workflow.
	if wfIdx, ok := m.hoveredWorkflow(); ok {
		return m.updateHoveredWorkflow(key, wfIdx)
	}

	switch {
	case bound(k, m.keys.Chats.Quit):
		return m, m.hide()
	case bound(k, m.keys.Chats.Back):
		return m, m.hide()
	case bound(k, m.keys.Chats.New):
		return m.openNewSession(nil)
	case bound(k, m.keys.Nav.Up):
		if m.chatCursor > 0 {
			m.chatCursor--
			m.refreshPreview()
		}
	case bound(k, m.keys.Nav.Down):
		if m.chatCursor < len(m.chatRows())-1 {
			m.chatCursor++
			m.refreshPreview()
		}
	case bound(k, m.keys.Chats.Open):
		item, ok := m.selectedChat()
		if !ok {
			return m, nil
		}
		return m.openAgent(item.agent)
	case bound(k, m.keys.Chats.Rename):
		item, ok := m.selectedChat()
		if !ok {
			return m, nil
		}
		m.renameTarget = item.agent
		m.form = renameForm(item.agent.Name, m.keys.Form)
		m.screen = screenRename
		return m, m.form.Init()
	case bound(k, m.keys.Chats.Delete):
		item, ok := m.selectedChat()
		if !ok {
			return m, nil
		}
		m.manager.Delete(item.agent)
		m.reload()
		m.chatCursor = clampCursor(m.chatCursor, len(m.chatRows()))
	}

	return m, nil
}

func (m *model) refreshPreview() {
	item, ok := m.selectedChat()
	if !ok {
		m.preview.SetContent("")
		return
	}

	// A dormant agent has no live pane to capture, so show its stored details.
	if !item.alive {
		m.preview.SetContent(dormantView(item.agent))
		m.preview.GotoTop()
		return
	}

	// The capture is the agent's current frame, sized to fill the box, so it is
	// shown as-is.
	m.preview.SetContent(m.manager.Preview(item.agent, m.preview.Width, m.preview.Height))
	m.preview.GotoTop()
}

// dormantView renders a dormant agent's stored properties in place of a live
// capture, since its pane is gone. It shows a fixed set of fields, with N/A for
// any that are blank, so the view has the same shape for every dormant chat and
// does not jitter as the selection moves.
func dormantView(agent store.Agent) string {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("#7E9CD8")).Bold(true)

	field := func(name, value string) string {
		return label.Render(name) + "  " + orNA(value) + "\n"
	}

	var b strings.Builder
	b.WriteString(dimStyle.Render("dormant — not running") + "\n\n")
	b.WriteString(field("title    ", agent.Name))
	b.WriteString(field("directory", agent.Cwd))
	b.WriteString(field("state    ", agent.State))
	b.WriteString(field("flags    ", agent.Flags))
	b.WriteString(field("resume   ", agent.ClaudeSessionID))
	b.WriteString("\n" + dimStyle.Render("enter to restore"))

	return b.String()
}

// resizePreview sizes the preview box to the popup's available area (minus the
// sidebar), which the hovered agent is then resized to fill.
func (m *model) resizePreview() {
	width := m.width - chatListWidth - 4
	if width < 10 {
		width = 10
	}

	height := m.height - 2
	if height < 3 {
		height = 3
	}

	// Leave room below the preview for the details footer (its text plus border).
	height -= detailsHeight + 2
	if height < 3 {
		height = 3
	}

	m.preview.Width = width
	m.preview.Height = height
	m.refreshPreview()
}

// chatDetails renders the hovered chat's stored record as a full-width footer
// below the picker and preview, so the fields the panel otherwise hides (working
// directory, flags, the resume id) are legible at a glance. The opening prompt
// is omitted: it is variable-length and wraps, which would jitter the footer.
func (m model) chatDetails() string {
	width := m.width - 2
	if width < 10 {
		width = 10
	}

	box := lipgloss.NewStyle().
		Width(width).
		Height(detailsHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#54546D"))

	// On a workflow header the footer is empty, but the box keeps its height so
	// the layout does not jitter as the cursor crosses folders.
	item, ok := m.selectedChat()
	if !ok {
		return box.Render("")
	}

	agent := item.agent
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("#7E9CD8")).Bold(true)

	field := func(name, value string) string {
		return label.Render(name) + "  " + orNA(value)
	}

	workflow := ""
	if agent.WorkflowID != nil {
		workflow = m.workflowName(*agent.WorkflowID)
	}

	lines := []string{
		field("started ", dates.Friendly(agent.CreatedAt)),
		field("workflow", workflow),
		field("dir     ", agent.Cwd),
		field("flags   ", agent.Flags),
		field("resume  ", agent.ClaudeSessionID),
	}

	return box.Render(strings.Join(lines, "\n"))
}

// actionHints renders the chat key bindings one per line with an aligned key
// column, so they fit inside the sidebar instead of overflowing its width.
func (m model) actionHints() string {
	hints := []struct{ key, desc string }{
		{hintKeys(m.keys.Chats.Open), "open"},
		{hintKeys(m.keys.Chats.New), "new chat"},
		{hintKeys(m.keys.Workflows.New), "new workflow"},
		{hintKeys(m.keys.Chats.Rename), "rename"},
		{hintKeys(m.keys.Chats.Delete), "delete"},
		{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		{hintKeys(m.keys.Chats.Back), "back"},
	}
	if _, hoveringWorkflow := m.hoveredWorkflow(); hoveringWorkflow {
		hints = []struct{ key, desc string }{
			{hintKeys(m.keys.Chats.New), "new chat"},
			{hintKeys(m.keys.Workflows.New), "new workflow"},
			{hintKeys(m.keys.Workflows.Rename), "rename"},
			{hintKeys(m.keys.Workflows.Edit), "edit"},
			{hintKeys(m.keys.Workflows.Delete), "delete"},
			{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		}
	} else if m.onNoWorkflowHeader() {
		hints = []struct{ key, desc string }{
			{hintKeys(m.keys.Chats.New), "new chat"},
			{hintKeys(m.keys.Workflows.New), "new workflow"},
			{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
			{hintKeys(m.keys.Chats.Back), "back"},
		}
	}

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

func (m model) chatsView() string {
	var list strings.Builder
	list.WriteString(headerStyle.Render("Chats") + "\n\n")

	// The list is a tree: each workflow is a folder header and its chats are
	// nested beneath it, so a chat's parent is shown by position rather than a
	// per-row marker.
	for i, r := range m.chatRows() {
		selected := i == m.chatCursor
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
			state = "" // dormant: neutral icon/color
			name = dimStyle.Render(item.agent.Name) + dimStyle.Render(" (dormant)")
			if selected {
				name = cursorStyle.Render(item.agent.Name) + dimStyle.Render(" (dormant)")
			}
		}

		icon := lipgloss.NewStyle().Foreground(stateColor(state)).Render(stateIcon(state))
		list.WriteString(prefix + "  " + icon + " " + name + "\n")
	}

	list.WriteString("\n" + m.actionHints())

	left := lipgloss.NewStyle().Width(chatListWidth).Render(list.String())
	right := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#54546D")).
		Render(m.preview.View())

	top := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	return lipgloss.JoinVertical(lipgloss.Left, top, m.chatDetails())
}
