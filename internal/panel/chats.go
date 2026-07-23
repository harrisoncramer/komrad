package panel

import (
	"strings"

	"claude-agents/internal/store"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const chatListWidth = 32

// updateChats drives the split chats view: arrows move the selection, letter
// keys act on the hovered chat, and ctrl+d/ctrl+u scroll the preview.
func (m model) updateChats(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	switch {
	case bound(k, m.keys.Chats.Quit):
		return m, tea.Quit
	case bound(k, m.keys.Chats.Back):
		m.screen = screenList
		return m, nil
	case bound(k, m.keys.Chats.Up):
		if m.chatCursor > 0 {
			m.chatCursor--
			m.refreshPreview()
		}
	case bound(k, m.keys.Chats.Down):
		if m.chatCursor < len(m.chats)-1 {
			m.chatCursor++
			m.refreshPreview()
		}
	case bound(k, m.keys.Chats.Open):
		agent := m.chats[m.chatCursor].agent
		m.attach = &agent
		return m, tea.Quit
	case bound(k, m.keys.Chats.Rename):
		agent := m.chats[m.chatCursor].agent
		m.renameTarget = agent
		m.form = renameForm(agent.Name, m.keys.Form)
		m.screen = screenRename
		return m, m.form.Init()
	case bound(k, m.keys.Chats.Delete):
		m.manager.Delete(m.client, m.chats[m.chatCursor].agent)
		m.reload()
		if m.chatCursor >= len(m.chats) {
			m.chatCursor = len(m.chats) - 1
		}
		if len(m.chats) == 0 {
			m.screen = screenList
		}
	case bound(k, m.keys.Chats.PreviewDown):
		m.scrollPreview(false)
	case bound(k, m.keys.Chats.PreviewUp):
		m.scrollPreview(true)
	}

	return m, nil
}

func (m *model) refreshPreview() {
	if m.chatCursor < 0 || m.chatCursor >= len(m.chats) {
		m.preview.SetContent("")
		return
	}

	item := m.chats[m.chatCursor]

	// A dormant agent has no live pane to capture, so show its stored details.
	if !item.alive {
		m.preview.SetContent(dormantView(item.agent))
		m.preview.GotoTop()
		return
	}

	// The capture is the agent's current frame, sized to fill the box, so it is
	// shown as-is. Scrolling is driven into the agent itself via scrollPreview,
	// and the next capture reflects it.
	m.preview.SetContent(m.manager.Preview(item.agent, m.preview.Width, m.preview.Height))
	m.preview.GotoTop()
}

// scrollPreview drives the hovered agent's own view with mouse-wheel events, then
// re-captures so the scroll shows in the box. Dormant agents have no pane to
// scroll.
func (m *model) scrollPreview(up bool) {
	if m.chatCursor < 0 || m.chatCursor >= len(m.chats) {
		return
	}

	item := m.chats[m.chatCursor]
	if !item.alive {
		return
	}

	m.manager.ScrollPreview(item.agent, m.preview.Width, m.preview.Height, up)
	m.refreshPreview()
}

// dormantView renders a dormant agent's stored properties in place of a live
// capture, since its pane is gone.
func dormantView(agent store.Agent) string {
	label := lipgloss.NewStyle().Foreground(lipgloss.Color("#7E9CD8")).Bold(true)

	field := func(name, value string) string {
		return label.Render(name) + "  " + value + "\n"
	}

	var b strings.Builder
	b.WriteString(dimStyle.Render("dormant — not running") + "\n\n")
	b.WriteString(field("title    ", agent.Name))
	b.WriteString(field("directory", agent.Cwd))
	b.WriteString(field("state    ", agent.State))
	if agent.Flags != "" {
		b.WriteString(field("flags    ", agent.Flags))
	}
	if agent.Prompt != "" {
		b.WriteString("\n" + label.Render("prompt") + "\n" + agent.Prompt + "\n")
	}

	resume := "no saved conversation to resume"
	if agent.ClaudeSessionID != "" {
		resume = "will resume conversation " + agent.ClaudeSessionID
	}
	b.WriteString("\n" + dimStyle.Render(resume))
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

	m.preview.Width = width
	m.preview.Height = height
	m.refreshPreview()
}

// actionHints renders the chat key bindings one per line with an aligned key
// column, so they fit inside the sidebar instead of overflowing its width.
func (m model) actionHints() string {
	hints := []struct{ key, desc string }{
		{hintKeys(m.keys.Chats.Open), "open"},
		{hintKeys(m.keys.Chats.Rename), "rename"},
		{hintKeys(m.keys.Chats.Delete), "delete"},
		{hintKeys(m.keys.Chats.Up) + "/" + hintKeys(m.keys.Chats.Down), "move"},
		{hintKeys(m.keys.Chats.PreviewDown) + "/" + hintKeys(m.keys.Chats.PreviewUp), "scroll"},
		{hintKeys(m.keys.Chats.Back), "back"},
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
	for i, item := range m.chats {
		cursor := "  "
		name := item.agent.Name
		if i == m.chatCursor {
			cursor = cursorStyle.Render("› ")
			name = cursorStyle.Render(name)
		}

		state := item.agent.State
		if !item.alive {
			state = "" // dormant: neutral icon/color
			name = dimStyle.Render(item.agent.Name) + dimStyle.Render(" (dormant)")
			if i == m.chatCursor {
				name = cursorStyle.Render(item.agent.Name) + dimStyle.Render(" (dormant)")
			}
		}

		icon := lipgloss.NewStyle().Foreground(stateColor(state)).Render(stateIcon(state))
		list.WriteString(cursor + icon + " " + name + "\n")
	}

	list.WriteString("\n" + m.actionHints())

	left := lipgloss.NewStyle().Width(chatListWidth).Render(list.String())
	right := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#54546D")).
		Render(m.preview.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
