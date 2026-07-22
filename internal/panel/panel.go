// Package panel is the bubbletea UI for browsing and acting on Claude agents.
package panel

import (
	"fmt"

	"claude-agents/internal/agents"
	"claude-agents/internal/registry"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Run launches the panel for a tmux client and blocks until it exits.
func Run(manager *agents.AgentManager, client string) error {
	if _, err := tea.NewProgram(newModel(manager, client), tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("failed to run panel: %w", err)
	}

	return nil
}

var (
	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7E9CD8")).Bold(true)
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7E9CD8")).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#727169"))
)

func stateIcon(state registry.State) string {
	switch state {
	case registry.StateDone:
		return "✓"
	case registry.StateThinking:
		return "…"
	case registry.StateNew:
		return "•"
	default:
		return "•"
	}
}

func stateColor(state registry.State) lipgloss.Color {
	switch state {
	case registry.StateDone:
		return lipgloss.Color("#E82424")
	case registry.StateThinking:
		return lipgloss.Color("#FF9E3B")
	case registry.StateNew:
		return lipgloss.Color("#7E9CD8")
	default:
		return lipgloss.Color("#7E9CD8")
	}
}

type screen int

const (
	screenList screen = iota
	screenChats
	screenForm
	screenConfirm
)

type rowKind int

const (
	rowNewChat rowKind = iota
	rowLast
	rowChats
)

type row struct {
	kind  rowKind
	label string
}

type model struct {
	manager *agents.AgentManager
	client  string

	screen screen
	rows   []row
	cursor int

	chats      []registry.Agent
	chatCursor int
	preview    viewport.Model

	agent      registry.Agent
	form       *huh.Form
	confirmYes bool

	width  int
	height int
}

func newModel(manager *agents.AgentManager, client string) model {
	chats := manager.List()

	return model{
		manager: manager,
		client:  client,
		screen:  screenList,
		rows:    buildRows(chats),
		chats:   chats,
		preview: viewport.New(0, 0),
	}
}

func buildRows(chats []registry.Agent) []row {
	return []row{
		{kind: rowNewChat, label: "+  new chat"},
		{kind: rowLast, label: "←  last window/session"},
		{kind: rowChats, label: fmt.Sprintf("▸  chats (%d)", len(chats))},
	}
}

func newSessionForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("title").Title("Title").Description("optional; claude names it if blank"),
			huh.NewInput().Key("dir").Title("Directory").Description("blank = current; a pre-create hook may override"),
			huh.NewInput().Key("flags").Title("Claude flags").Placeholder("--model opus"),
			huh.NewText().Key("prompt").Title("Initial prompt").Description("ctrl+e opens $EDITOR"),
		),
	).WithShowHelp(true)
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
		m.resizePreview()
		return m, nil
	}

	if m.screen == screenForm {
		return m.updateForm(msg)
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch m.screen {
	case screenList:
		return m.updateList(key)
	case screenChats:
		return m.updateChats(key)
	case screenConfirm:
		return m.updateConfirm(key)
	default:
		return m, nil
	}
}

func (m model) updateList(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case "enter":
		return m.selectRow(m.rows[m.cursor])
	}

	return m, nil
}

func (m model) selectRow(selected row) (tea.Model, tea.Cmd) {
	switch selected.kind {
	case rowNewChat:
		m.form = newSessionForm()
		m.screen = screenForm
		return m, m.form.Init()
	case rowLast:
		m.manager.Last(m.client)
		return m, tea.Quit
	case rowChats:
		if len(m.chats) == 0 {
			return m, nil
		}

		m.screen = screenChats
		m.chatCursor = 0
		m.refreshPreview()
	}

	return m, nil
}

func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		m.manager.NewSession(agents.NewSessionParams{
			Client: m.client,
			Title:  m.form.GetString("title"),
			Dir:    m.form.GetString("dir"),
			Flags:  m.form.GetString("flags"),
			Prompt: m.form.GetString("prompt"),
		})
		return m, tea.Quit
	case huh.StateAborted:
		m.screen = screenList
		return m, nil
	default:
		return m, cmd
	}
}

func (m model) updateConfirm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "n":
		m.screen = screenChats
		return m, nil
	case "left", "right", "h", "l", "tab":
		m.confirmYes = !m.confirmYes
	case "y":
		return m.applyDestroy()
	case "enter":
		if m.confirmYes {
			return m.applyDestroy()
		}

		m.screen = screenChats
		return m, nil
	}

	return m, nil
}

// applyDestroy kills the selected agent, reloads, and returns to the chats view
// (or the top list when none remain).
func (m model) applyDestroy() (tea.Model, tea.Cmd) {
	m.manager.Destroy(m.client, m.agent)
	m.chats = m.manager.List()
	m.rows = buildRows(m.chats)

	if len(m.chats) == 0 {
		m.screen = screenList
		return m, nil
	}

	if m.chatCursor >= len(m.chats) {
		m.chatCursor = len(m.chats) - 1
	}

	m.screen = screenChats
	m.refreshPreview()
	return m, nil
}
