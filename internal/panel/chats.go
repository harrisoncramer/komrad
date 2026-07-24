package panel

import (
	"fmt"
	"strings"

	"claude-agents/internal/store"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// noWorkflowLabel heads the trailing group of chats that belong to no workflow.
const noWorkflowLabel = "No Workflow"

// chatRow is one line of the chats list. It is either a workflow header or a
// chat nested beneath one. The cursor moves over both but actions apply only to
// chat rows.
type chatRow struct {
	header bool
	label  string   // workflow name or noWorkflowLabel when header is true
	item   chatItem // the chat when header is false
	// workflowIndex is the header's index in m.workflows so workflow actions can
	// target it. It is -1 for the no-workflow header and unused for chat rows.
	workflowIndex int
}

// chatRows arranges the chats into the displayed tree. Every workflow is a
// header followed by its chats, then a trailing group of chats that belong to no
// workflow. Empty workflows still get a header so a new one can be hovered to add
// its first chat.
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

	// Chats whose workflow is unknown, for example one just deleted, fall into the
	// unassigned group rather than vanishing.
	for _, leftover := range byWorkflow {
		unassigned = append(unassigned, leftover...)
	}
	rows = append(rows, chatRow{header: true, label: noWorkflowLabel, workflowIndex: -1})
	for _, it := range unassigned {
		rows = append(rows, chatRow{item: it})
	}

	return rows
}

// selectedChat returns the chat under the cursor, or false when the cursor is on
// a workflow header or the list is empty.
func (m model) selectedChat() (chatItem, bool) {
	rows := m.chatRows()
	if m.chatCursor < 0 || m.chatCursor >= len(rows) || rows[m.chatCursor].header {
		return chatItem{}, false
	}

	return rows[m.chatCursor].item, true
}

// hoveredWorkflow returns the m.workflows index of the workflow header under the
// cursor and true, or false when the cursor is not on a real workflow header.
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
// header so entering the list lands on a chat rather than a folder label.
func firstChatRowIndex(rows []chatRow) int {
	for i, r := range rows {
		if !r.header {
			return i
		}
	}

	return 0
}

// rowIndexOfChatName returns the row of the named chat, or -1 when it is gone, so
// a refresh can keep the cursor on the same chat as the tree is rebuilt.
func rowIndexOfChatName(rows []chatRow, name string) int {
	for i, r := range rows {
		if !r.header && r.item.agent.Name == name {
			return i
		}
	}

	return -1
}

// updateChats drives the nav list. Arrows move the selection and restage, and
// letter keys act on the hovered chat or workflow.
func (m model) updateChats(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// New workflow is global to the chats view regardless of cursor position, so it
	// never collides with the per-row new-chat key.
	if bound(k, m.keys.Workflows.New) {
		return m.newWorkflowForm()
	}

	// Search is likewise global to the chats view. It opens over the whole corpus,
	// not the hovered row.
	if bound(k, m.keys.Chats.Search) {
		return m.enterSearch()
	}

	// On a workflow header the workflow bindings act on that folder's workflow.
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
			m.stageSelection()
		}
	case bound(k, m.keys.Nav.Down):
		if m.chatCursor < len(m.chatRows())-1 {
			m.chatCursor++
			m.stageSelection()
		}
	case bound(k, m.keys.Chats.Open):
		return m.openChat()
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
		m.stageSelection()
	}

	return m, nil
}

// openChat acts on the hovered chat. A dormant chat is restored and staged live.
// A live chat is already staged from hovering, so opening it is a no-op. Moving
// focus onto it is a plain tmux pane move the user drives.
func (m model) openChat() (tea.Model, tea.Cmd) {
	item, ok := m.selectedChat()
	if !ok {
		return m, nil
	}
	if item.alive {
		return m, nil
	}

	pane, err := m.manager.Restore(item.agent)
	if err != nil {
		return m, nil
	}
	m.reload()
	m.chatCursor = clampCursor(rowIndexOfChatName(m.chatRows(), item.agent.Name), len(m.chatRows()))
	m.manager.StageChat(pane)

	return m, nil
}

// stageSelection updates the stage pane to reflect what the cursor is on. A live
// chat's pane is swapped in, a dormant chat and a workflow header show markdown
// via bat, and anything else blanks the stage.
func (m model) stageSelection() {
	rows := m.chatRows()
	if m.chatCursor < 0 || m.chatCursor >= len(rows) {
		m.manager.StageBlank()
		return
	}

	r := rows[m.chatCursor]
	switch {
	case r.header && r.workflowIndex >= 0:
		m.manager.StageText(workflowMarkdown(m.workflows[r.workflowIndex], m.chatCount(m.workflows[r.workflowIndex].ID)))
	case r.header:
		m.manager.StageBlank()
	case r.item.alive:
		m.manager.StageChat(r.item.agent.Pane)
	default:
		m.manager.StageText(dormantMarkdown(r.item.agent))
	}
}

// chatCount returns how many listed chats belong to the given workflow id.
func (m model) chatCount(workflowID string) int {
	n := 0
	for _, it := range m.chats {
		if it.agent.WorkflowID != nil && *it.agent.WorkflowID == workflowID {
			n++
		}
	}

	return n
}

func orDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}

	return v
}

// workflowMarkdown renders a workflow's info for the stage. It shows the name,
// shared context, and config through bat.
func workflowMarkdown(w store.Workflow, chats int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", w.Name)
	if strings.TrimSpace(w.Context) != "" {
		b.WriteString(w.Context + "\n\n")
	} else {
		b.WriteString("_no shared context set_\n\n")
	}
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "- chats: %d\n", chats)
	fmt.Fprintf(&b, "- directory: %s\n", orDash(w.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(w.Flags))

	return b.String()
}

// dormantMarkdown renders a dormant chat's stored record for the stage, since it
// has no live pane to show.
func dormantMarkdown(a store.Agent) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n_dormant — not running_\n\n", a.Name)
	b.WriteString("---\n\n")
	fmt.Fprintf(&b, "- directory: %s\n", orDash(a.Cwd))
	fmt.Fprintf(&b, "- flags: %s\n", orDash(a.Flags))
	fmt.Fprintf(&b, "- resume: %s\n\n", orDash(a.ClaudeSessionID))
	b.WriteString("Press open to restore.\n")

	return b.String()
}

// actionHints renders the key bindings one per line with an aligned key column.
func (m model) actionHints() string {
	hints := []struct{ key, desc string }{
		{hintKeys(m.keys.Chats.Open), "open"},
		{hintKeys(m.keys.Chats.New), "new chat"},
		{hintKeys(m.keys.Workflows.New), "new workflow"},
		{hintKeys(m.keys.Chats.Rename), "rename"},
		{hintKeys(m.keys.Chats.Delete), "delete"},
		{hintKeys(m.keys.Chats.Search), "search"},
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

// chatsView renders the nav list, the panel's own pane. The hovered resource is
// shown in the separate stage pane, not here.
func (m model) chatsView() string {
	var list strings.Builder
	list.WriteString(headerStyle.Render("Chats") + "\n\n")

	// The list is a tree. Each workflow is a folder header with its chats nested
	// beneath it, so a chat's parent is shown by position rather than a per-row
	// marker.
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
			state = "" // dormant, neutral icon and color
			name = dimStyle.Render(item.agent.Name)
			if selected {
				name = cursorStyle.Render(item.agent.Name)
			}
		}

		icon := lipgloss.NewStyle().Foreground(stateColor(state)).Render(stateIcon(state))
		list.WriteString(prefix + "  " + icon + " " + name + "\n")
	}

	list.WriteString("\n" + m.actionHints())

	return list.String()
}
