// Package panel is the bubbletea UI for browsing and acting on Claude agents.
package panel

import (
	"fmt"

	"claude-agents/internal/agents"
	"claude-agents/internal/registry"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Run launches the panel for a tmux client and blocks until it exits.
func Run(manager *agents.AgentManager, client string) error {
	if _, err := tea.NewProgram(newModel(manager, client)).Run(); err != nil {
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
	screenAction
	screenForm
	screenConfirm
)

type rowKind int

const (
	rowNewChat rowKind = iota
	rowLast
	rowAgent
)

type row struct {
	kind  rowKind
	label string
	agent registry.Agent
}

type model struct {
	manager *agents.AgentManager
	client  string

	screen screen
	rows   []row
	cursor int

	agent       registry.Agent
	actions     []string
	actionIndex int

	form       *huh.Form
	confirmYes bool
}

func newModel(manager *agents.AgentManager, client string) model {
	return model{
		manager: manager,
		client:  client,
		screen:  screenList,
		rows:    buildRows(manager.List()),
	}
}

func buildRows(list []registry.Agent) []row {
	rows := []row{
		{kind: rowNewChat, label: "+  new chat"},
		{kind: rowLast, label: "←  last window/session"},
	}
	for _, agent := range list {
		rows = append(rows, row{kind: rowAgent, agent: agent})
	}

	return rows
}

func newSessionForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("title").Title("Title").Description("optional; claude names it if blank"),
			huh.NewInput().Key("flags").Title("Claude flags").Placeholder("--model opus"),
			huh.NewText().Key("prompt").Title("Initial prompt").Description("ctrl+e opens $EDITOR"),
		),
	).WithShowHelp(true)
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case screenAction:
		return m.updateAction(key)
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
	case rowAgent:
		m.agent = selected.agent
		m.actions = []string{"jump", "chat", "destroy"}
		m.actionIndex = 0
		m.screen = screenAction
	}

	return m, nil
}

func (m model) updateAction(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "esc", "left", "h":
		m.screen = screenList
	case "up", "k":
		if m.actionIndex > 0 {
			m.actionIndex--
		}
	case "down", "j":
		if m.actionIndex < len(m.actions)-1 {
			m.actionIndex++
		}
	case "enter":
		return m.runAction(m.actions[m.actionIndex])
	}

	return m, nil
}

func (m model) runAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "jump":
		m.manager.Jump(m.client, m.agent)
		return m, tea.Quit
	case "chat":
		m.manager.Chat(m.agent)
		return m, tea.Quit
	case "destroy":
		m.confirmYes = false
		m.screen = screenConfirm
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
	case "ctrl+c", "esc", "n":
		return m, tea.Quit
	case "left", "right", "h", "l", "tab":
		m.confirmYes = !m.confirmYes
	case "y":
		m.manager.Destroy(m.client, m.agent)
		return m, tea.Quit
	case "enter":
		if m.confirmYes {
			m.manager.Destroy(m.client, m.agent)
		}

		return m, tea.Quit
	}

	return m, nil
}
