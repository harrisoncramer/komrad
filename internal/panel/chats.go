package panel

import (
	"strings"

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
		m.manager.Jump(m.client, m.chats[m.chatCursor])
		return m, tea.Quit
	case "c":
		m.manager.Chat(m.chats[m.chatCursor])
		return m, tea.Quit
	case "x":
		m.agent = m.chats[m.chatCursor]
		m.confirmYes = false
		m.screen = screenConfirm
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

	// Follow the tail while pinned to the bottom, but hold position when the
	// user has scrolled up.
	atBottom := m.preview.AtBottom()
	offset := m.preview.YOffset

	m.preview.SetContent(m.manager.Preview(m.chats[m.chatCursor]))

	if atBottom {
		m.preview.GotoBottom()
	} else {
		m.preview.SetYOffset(offset)
	}
}

// resizePreview sizes the preview box to the agents' fixed size so a capture
// fits exactly, capped to the space the popup actually has.
func (m *model) resizePreview() {
	width := m.agentWidth
	if avail := m.width - chatListWidth - 4; width > avail {
		width = avail
	}
	if width < 10 {
		width = 10
	}

	height := m.agentHeight
	if avail := m.height - 2; height > avail {
		height = avail
	}
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
	for i, agent := range m.chats {
		cursor := "  "
		name := agent.Session
		if i == m.chatCursor {
			cursor = cursorStyle.Render("› ")
			name = cursorStyle.Render(name)
		}

		icon := lipgloss.NewStyle().Foreground(stateColor(agent.State)).Render(stateIcon(agent.State))
		list.WriteString(cursor + icon + " " + name + "\n")
	}

	list.WriteString("\n" + dimStyle.Render("enter/l jump · c chat · x kill\nj/k move · C-d/C-u scroll · esc back"))

	left := lipgloss.NewStyle().Width(chatListWidth).Render(list.String())
	right := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#54546D")).
		Render(m.preview.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
