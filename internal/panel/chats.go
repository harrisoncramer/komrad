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
	switch key.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "left", "h":
		m.screen = screenList
		return m, nil
	case "up", "k":
		if m.chatCursor > 0 {
			m.chatCursor--
			m.refreshPreview()
		}
	case "down", "j":
		if m.chatCursor < len(m.chats)-1 {
			m.chatCursor++
			m.refreshPreview()
		}
	case "enter", "l", "right":
		agent := m.chats[m.chatCursor].agent
		m.attach = &agent
		return m, tea.Quit
	case "r":
		agent := m.chats[m.chatCursor].agent
		m.renameTarget = agent
		m.form = renameForm(agent.Name)
		m.screen = screenRename
		return m, m.form.Init()
	case "d":
		m.manager.Delete(m.client, m.chats[m.chatCursor].agent)
		m.reload()
		if m.chatCursor >= len(m.chats) {
			m.chatCursor = len(m.chats) - 1
		}
		if len(m.chats) == 0 {
			m.screen = screenList
		}
	case "ctrl+d":
		m.preview.HalfPageDown()
	case "ctrl+u":
		m.preview.HalfPageUp()
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

	// Follow the tail while pinned to the bottom, but hold position when the
	// user has scrolled up.
	atBottom := m.preview.AtBottom()
	offset := m.preview.YOffset

	m.preview.SetContent(m.manager.Preview(item.agent, m.preview.Width, m.preview.Height))

	if atBottom {
		m.preview.GotoBottom()
	} else {
		m.preview.SetYOffset(offset)
	}
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

	list.WriteString("\n" + dimStyle.Render("enter/l open · r rename · d delete\nj/k move · C-d/C-u scroll · esc back"))

	left := lipgloss.NewStyle().Width(chatListWidth).Render(list.String())
	right := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#54546D")).
		Render(m.preview.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
