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
		agent := m.chats[m.chatCursor].agent
		m.attach = &agent
		return m, tea.Quit
	case "x":
		m.agent = m.chats[m.chatCursor].agent
		m.confirmYes = false
		m.screen = screenConfirm
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

	// Follow the tail while pinned to the bottom, but hold position when the
	// user has scrolled up.
	atBottom := m.preview.AtBottom()
	offset := m.preview.YOffset

	m.preview.SetContent(m.manager.Preview(m.chats[m.chatCursor].agent, m.preview.Width, m.preview.Height))

	if atBottom {
		m.preview.GotoBottom()
	} else {
		m.preview.SetYOffset(offset)
	}
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

	list.WriteString("\n" + dimStyle.Render("enter/l open · x kill · d delete\nj/k move · C-d/C-u scroll · esc back"))

	left := lipgloss.NewStyle().Width(chatListWidth).Render(list.String())
	right := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#54546D")).
		Render(m.preview.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
