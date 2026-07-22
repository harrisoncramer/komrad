// Package panel is the bubbletea UI for browsing and acting on Claude agents.
package panel

import (
	"context"
	"fmt"
	"time"

	"claude-agents/internal/agents"
	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"
	"claude-agents/proto/gen/session/v1/sessionv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/protobuf/types/known/emptypb"
)

// previewInterval is how often the chats preview re-captures the hovered pane.
const previewInterval = 200 * time.Millisecond

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(previewInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// Run launches the panel and blocks until it exits. Sessions are read through
// the ConnectRPC client. If the user opens an agent, Run returns it so the
// caller can attach in place; otherwise it returns nil.
func Run(manager *agents.AgentManager, sessions sessionv1connect.SessionServiceClient, client string) (*store.Agent, error) {
	final, err := tea.NewProgram(newModel(manager, sessions, client), tea.WithAltScreen()).Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run panel: %w", err)
	}

	m, ok := final.(model)
	if !ok {
		return nil, nil
	}

	return m.attach, nil
}

var (
	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7E9CD8")).Bold(true)
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7E9CD8")).Bold(true)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#727169"))
)

func stateIcon(state string) string {
	switch state {
	case store.StateDone:
		return "✓"
	case store.StateThinking:
		return "…"
	default:
		return "•"
	}
}

func stateColor(state string) lipgloss.Color {
	switch state {
	case store.StateDone:
		return lipgloss.Color("#E82424")
	case store.StateThinking:
		return lipgloss.Color("#FF9E3B")
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
	rowChats
)

type row struct {
	kind  rowKind
	label string
}

type model struct {
	manager  *agents.AgentManager
	sessions sessionv1connect.SessionServiceClient
	client   string

	screen screen
	rows   []row
	cursor int

	chats      []chatItem
	chatCursor int
	preview    viewport.Model

	agent      store.Agent
	attach     *store.Agent
	form       *huh.Form
	confirmYes bool

	width  int
	height int
}

func newModel(manager *agents.AgentManager, sessions sessionv1connect.SessionServiceClient, client string) model {
	m := model{
		manager:  manager,
		sessions: sessions,
		client:   client,
		screen:   screenList,
		preview:  viewport.New(0, 0),
	}
	m.reload()

	return m
}

// chatItem is a stored agent plus whether its tmux pane is currently alive. A
// dormant agent (dead pane) is kept — it can be restored from its stored state.
type chatItem struct {
	agent store.Agent
	alive bool
}

// reload lists every stored agent through the service, marking which are live.
// Records are never deleted here, so agents survive tmux restarts.
func (m *model) reload() {
	resp, err := m.sessions.ListSessions(context.Background(), connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		m.chats = nil
		m.rows = buildRows(0)
		return
	}

	items := make([]chatItem, 0, len(resp.Msg.GetSessions()))
	for _, session := range resp.Msg.GetSessions() {
		agent := protoToAgent(session)
		items = append(items, chatItem{agent: agent, alive: m.manager.PaneAlive(agent.Pane)})
	}

	m.chats = items
	m.rows = buildRows(len(items))
}

func protoToAgent(s *sessionv1.Session) store.Agent {
	return store.Agent{
		ID:     s.GetId(),
		Name:   s.GetName(),
		Pane:   s.GetPaneId(),
		Cwd:    s.GetCwd(),
		Flags:  s.GetFlags(),
		Prompt: s.GetPrompt(),
		State:  s.GetState(),
	}
}

func buildRows(count int) []row {
	return []row{
		{kind: rowNewChat, label: "+  new chat"},
		{kind: rowChats, label: fmt.Sprintf("▸  chats (%d)", count)},
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

	if _, ok := msg.(tickMsg); ok {
		if m.screen != screenChats {
			return m, nil
		}

		m.refreshPreview()
		return m, tick()
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
	case rowChats:
		if len(m.chats) == 0 {
			return m, nil
		}

		m.screen = screenChats
		m.chatCursor = 0
		m.refreshPreview()
		return m, tick()
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
		m.refreshPreview()
		return m, tick()
	case "left", "right", "h", "l", "tab":
		m.confirmYes = !m.confirmYes
	case "y":
		return m.applyDestroy()
	case "enter":
		if m.confirmYes {
			return m.applyDestroy()
		}

		m.screen = screenChats
		m.refreshPreview()
		return m, tick()
	}

	return m, nil
}

// applyDestroy kills the selected agent, reloads, and returns to the chats view
// (or the top list when none remain).
func (m model) applyDestroy() (tea.Model, tea.Cmd) {
	m.manager.Destroy(m.client, m.agent)
	m.reload()

	if len(m.chats) == 0 {
		m.screen = screenList
		return m, nil
	}

	if m.chatCursor >= len(m.chats) {
		m.chatCursor = len(m.chats) - 1
	}

	m.screen = screenChats
	m.refreshPreview()
	return m, tick()
}
