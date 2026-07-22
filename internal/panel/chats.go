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
		m.closeLive()
		return m, tea.Quit
	case "esc", "left", "h":
		m.closeLive()
		m.screen = screenList
		return m, nil
	case "up", "k":
		if m.chatCursor > 0 {
			m.chatCursor--
			m.ensureLive()
			m.refreshPreview()
		}
	case "down", "j":
		if m.chatCursor < len(m.chats)-1 {
			m.chatCursor++
			m.ensureLive()
			m.refreshPreview()
		}
	case "enter", "l", "right":
		m.closeLive()
		m.manager.Jump(m.client, m.chats[m.chatCursor])
		return m, tea.Quit
	case "c":
		m.closeLive()
		m.manager.Chat(m.chats[m.chatCursor])
		return m, tea.Quit
	case "x":
		m.closeLive()
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

// ensureLive attaches a control-mode live view to the hovered agent, replacing
// any previous one when the selection changes.
func (m *model) ensureLive() {
	if m.chatCursor < 0 || m.chatCursor >= len(m.chats) {
		m.closeLive()
		return
	}

	agent := m.chats[m.chatCursor]
	key := agent.Session + "\x00" + agent.Pane
	if m.live != nil && key == m.liveKey {
		return
	}

	m.closeLive()

	client, err := m.manager.AttachPreview(agent, m.preview.Width, m.preview.Height)
	if err != nil {
		return
	}

	m.live = client
	m.liveKey = key
}

func (m *model) closeLive() {
	if m.live != nil {
		m.live.Close()
		m.live = nil
		m.liveKey = ""
	}
}

func (m *model) refreshPreview() {
	if m.live != nil {
		m.preview.SetContent(m.live.Render())
		return
	}

	m.preview.SetContent("")
}

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

	list.WriteString("\n" + dimStyle.Render("enter/l jump · c chat · x kill\nj/k move · esc back"))

	left := lipgloss.NewStyle().Width(chatListWidth).Render(list.String())
	right := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#54546D")).
		Render(m.preview.View())

	return lipgloss.JoinHorizontal(lipgloss.Top, left, right)
}
