// Package panel is the bubbletea UI for browsing and acting on Claude agents.
package panel

import (
	"context"
	"fmt"
	"time"

	"claude-agents/internal/agents"
	"claude-agents/internal/appconfig"
	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"
	"claude-agents/proto/gen/session/v1/sessionv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/protobuf/types/known/emptypb"
)

// chatsInterval is how often the chats list re-lists over RPC. It runs off the
// render loop, so a slow serve can never freeze the UI.
const (
	chatsInterval = time.Second
	chatsTimeout  = 2 * time.Second
)

type chatsTickMsg struct{}

// chatsLoadedMsg carries the result of an async re-list. ok is false when the
// list failed, so the handler leaves the current chats in place rather than
// clobbering them to empty.
type chatsLoadedMsg struct {
	items []chatItem
	ok    bool
}

func chatsTick() tea.Cmd {
	return tea.Tick(chatsInterval, func(time.Time) tea.Msg { return chatsTickMsg{} })
}

// loadChatsCmd re-lists the sessions off the UI goroutine with a timeout, so the
// render loop never blocks on the RPC or the per-chat tmux liveness checks.
func (m model) loadChatsCmd() tea.Cmd {
	sessions, manager := m.sessions, m.manager

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), chatsTimeout)
		defer cancel()

		listed, err := listSessions(ctx, sessions)
		if err != nil {
			return chatsLoadedMsg{ok: false}
		}

		return chatsLoadedMsg{items: chatItemsFrom(listed, manager), ok: true}
	}
}

// listSessions lists every chat through the service.
func listSessions(ctx context.Context, sessions sessionv1connect.SessionServiceClient) ([]*sessionv1.Session, error) {
	resp, err := sessions.ListSessions(ctx, connect.NewRequest(&emptypb.Empty{}))
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	return resp.Msg.GetSessions(), nil
}

// chatItemsFrom maps listed sessions to chat items, marking which panes are live.
func chatItemsFrom(sessions []*sessionv1.Session, manager *agents.AgentManager) []chatItem {
	items := make([]chatItem, 0, len(sessions))
	for _, session := range sessions {
		agent := protoToAgent(session)
		items = append(items, chatItem{agent: agent, alive: manager.PaneAlive(agent.Pane)})
	}

	return items
}

// Run launches the panel and blocks for the life of the persistent manager
// session. Sessions are read through the ConnectRPC client. The panel is the nav
// pane. The resource it hovers is shown in a sibling stage pane, and focus moves
// between the two with the user's own tmux bindings.
func Run(manager *agents.AgentManager, sessions sessionv1connect.SessionServiceClient, keys appconfig.Keybindings) error {
	if _, err := tea.NewProgram(newModel(manager, sessions, keys), tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("failed to run panel: %w", err)
	}

	return nil
}

// hide detaches the popup's client from the persistent manager session without
// stopping the panel. The bubbletea program keeps running with its state intact,
// so reopening the popup restores the exact screen. The close and quit keys hide
// the popup rather than tear the panel down.
func (m model) hide() tea.Cmd {
	return func() tea.Msg {
		m.manager.HideManager()
		return nil
	}
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
	case store.StateWaiting:
		return "!"
	case store.StateThinking:
		return "…"
	default:
		return "•"
	}
}

func stateColor(state string) lipgloss.Color {
	switch state {
	case store.StateDone:
		return lipgloss.Color("#98BB6C")
	case store.StateWaiting:
		return lipgloss.Color("#E82424")
	case store.StateThinking:
		return lipgloss.Color("#FF9E3B")
	default:
		return lipgloss.Color("#7E9CD8")
	}
}

type screen int

const (
	screenChats screen = iota
	screenForm
	screenRename
	screenWorkflowForm
	screenWorkflowDelete
	screenSearch
)

type model struct {
	manager  *agents.AgentManager
	sessions sessionv1connect.SessionServiceClient

	screen screen

	chats      []chatItem
	chatCursor int

	// workflows backs the tree's folder headers. workflowCursor is which workflow a
	// header-hover action targets. workflowRenameTarget and workflowEditTarget are
	// the workflows being renamed or edited while the workflow form is open, both
	// nil when creating one. workflowDeleteTarget is the one the delete prompt is
	// confirming.
	workflows            []store.Workflow
	workflowCursor       int
	workflowRenameTarget *store.Workflow
	workflowEditTarget   *store.Workflow
	workflowDeleteTarget *store.Workflow

	// newChatWorkflow is the workflow a pending new-chat form will place its chat
	// in, or nil for a chat that belongs to no workflow. Set when the form opens
	// and consumed on submit.
	newChatWorkflow *store.Workflow

	form         *huh.Form
	renameTarget store.Agent

	// Search screen state. The live query, its hits, and which hit is hovered.
	searchQuery  string
	searchHits   []searchHit
	searchCursor int

	keys appconfig.Keybindings
}

func newModel(manager *agents.AgentManager, sessions sessionv1connect.SessionServiceClient, keys appconfig.Keybindings) model {
	m := model{
		manager:  manager,
		sessions: sessions,
		keys:     keys,
		screen:   screenChats,
	}
	// Split off the stage pane beside the panel, then show whatever the initial
	// selection lands on.
	m.manager.InitStage()
	m.reload()
	m.chatCursor = firstChatRowIndex(m.chatRows())
	m.stageSelection()

	return m
}

// chatItem is a stored agent plus whether its tmux pane is currently alive. A
// dormant agent has a dead pane but is kept, since it can be restored from its stored state.
type chatItem struct {
	agent store.Agent
	alive bool
}

// reload lists the agents through the service, marking which are live, and
// refreshes the workflow list. Records are never deleted here, so agents
// survive tmux restarts.
func (m *model) reload() {
	m.reloadWorkflows()

	listed, err := listSessions(context.Background(), m.sessions)
	if err != nil {
		m.chats = nil
		return
	}

	m.chats = chatItemsFrom(listed, m.manager)
}

// reloadWorkflows refreshes the workflow list backing the tree's folder headers,
// clamping the cursor into the new length.
func (m *model) reloadWorkflows() {
	workflows, err := m.manager.ListWorkflows()
	if err != nil {
		m.workflows = nil
		m.workflowCursor = 0
		return
	}

	m.workflows = workflows
	m.workflowCursor = clampCursor(m.workflowCursor, len(workflows))
}

// applyChats merges a freshly-listed set into the on-screen chats. It keeps the
// current order rather than re-sorting by recency, since reordering under the
// cursor is jarring and defeats an index-based selection. New chats are appended
// and vanished ones dropped, and the cursor stays on whichever chat it was on.
// The fresh items carry updated state and liveness, so a reconciler change
// shows without a structural reload.
func (m *model) applyChats(fresh []chatItem) {
	byName := make(map[string]chatItem, len(fresh))
	order := make([]string, 0, len(fresh))
	for _, item := range fresh {
		byName[item.agent.Name] = item
		order = append(order, item.agent.Name)
	}

	selected := ""
	if item, ok := m.selectedChat(); ok {
		selected = item.agent.Name
	}

	seen := make(map[string]bool, len(m.chats))
	items := make([]chatItem, 0, len(fresh))
	for _, cur := range m.chats {
		if item, ok := byName[cur.agent.Name]; ok {
			seen[cur.agent.Name] = true
			items = append(items, item)
		}
	}
	for _, name := range order {
		if !seen[name] {
			items = append(items, byName[name])
		}
	}

	m.chats = items

	// The cursor indexes the tree of headers and chats, so keep it on the same
	// chat across the rebuild, or clamp it into the new tree when that chat is
	// gone.
	rows := m.chatRows()
	if idx := rowIndexOfChatName(rows, selected); idx >= 0 {
		m.chatCursor = idx
	} else {
		m.chatCursor = clampCursor(m.chatCursor, len(rows))
	}
}

// indexOfChat returns the position of the named chat, or 0 when it is not found,
// so the cursor lands on the first row.
func indexOfChat(items []chatItem, name string) int {
	for i, item := range items {
		if item.agent.Name == name {
			return i
		}
	}

	return 0
}

// clampCursor keeps an index within a list of the given length.
func clampCursor(idx, length int) int {
	if idx >= length {
		idx = length - 1
	}
	if idx < 0 {
		idx = 0
	}

	return idx
}

func protoToAgent(s *sessionv1.Session) store.Agent {
	created, _ := time.Parse(time.RFC3339, s.GetCreatedAt())

	return store.Agent{
		ID:              s.GetId(),
		Name:            s.GetName(),
		Pane:            s.GetPaneId(),
		Cwd:             s.GetCwd(),
		Flags:           s.GetFlags(),
		Prompt:          s.GetPrompt(),
		State:           s.GetState(),
		ClaudeSessionID: s.GetClaudeSessionId(),
		WorkflowID:      s.WorkflowId,
		CreatedAt:       created,
	}
}

// openNewSession opens the new-chat form, seeded from the given workflow's
// blueprint when one is passed, and records that workflow so the created chat is
// placed in it. A nil workflow creates a chat that belongs to none.
func (m model) openNewSession(workflow *store.Workflow) (tea.Model, tea.Cmd) {
	dir, flags := "", ""
	if workflow != nil {
		dir = workflow.Cwd
		flags = workflow.Flags
	}
	m.newChatWorkflow = workflow
	m.form = newSessionForm(m.keys.Form, dir, flags)
	m.screen = screenForm

	return m, m.form.Init()
}

func renameForm(current string, keys appconfig.FormKeys) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(fmt.Sprintf("Rename %q to", current)),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys))
}

func (m model) updateRename(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		m.manager.Rename(m.renameTarget, m.form.GetString("name"))
		m.reload()
		m.chatCursor = clampCursor(m.chatCursor, len(m.chatRows()))
		m.screen = screenChats
		m.stageSelection()
		return m, nil
	case huh.StateAborted:
		m.screen = screenChats
		m.stageSelection()
		return m, nil
	default:
		return m, cmd
	}
}

// newSessionForm builds the new-chat form. defaultDir and defaultFlags prefill
// the directory and flags fields from the enclosing workflow's config so a chat
// created inside a workflow starts from its blueprint. Both are blank at the root.
func newSessionForm(keys appconfig.FormKeys, defaultDir, defaultFlags string) *huh.Form {
	dir := defaultDir
	flags := defaultFlags

	return huh.NewForm(
		huh.NewGroup(
			huh.NewText().Key("prompt").Title("Initial prompt").Description("ctrl+e opens $EDITOR; shift+enter submits"),
			huh.NewInput().Key("title").Title("Title").Description("optional; claude names it if blank"),
			huh.NewInput().Key("dir").Title("Directory").Description("blank = current").Value(&dir),
			huh.NewInput().Key("flags").Title("Claude flags").Placeholder("--model opus").Value(&flags),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys))
}

// formKeyMap is the default form keymap with the configured back and quit keys
// bound to abort, so esc backs out of a form the same way it leaves other
// screens.
func formKeyMap(keys appconfig.FormKeys) *huh.KeyMap {
	quit := append(append([]string{}, keys.Back...), keys.Quit...)

	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys(quit...), key.WithHelp(hintKeys(keys.Back), "back"))
	return km
}

// Init starts the async chats refresh so the list's counts and states stay current.
func (m model) Init() tea.Cmd {
	return tea.Batch(chatsTick(), m.loadChatsCmd())
}

// Update routes messages and key input to the active screen.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(chatsTickMsg); ok {
		return m, tea.Batch(m.loadChatsCmd(), chatsTick())
	}

	if loaded, ok := msg.(chatsLoadedMsg); ok {
		if loaded.ok {
			m.applyChats(loaded.items)
		}

		return m, nil
	}

	if results, ok := msg.(searchResultsMsg); ok {
		return m, m.applySearchResults(results)
	}

	if loaded, ok := msg.(transcriptLoadedMsg); ok {
		m.applyTranscript(loaded)
		return m, nil
	}

	if m.screen == screenForm {
		return m.updateForm(msg)
	}

	if m.screen == screenRename {
		return m.updateRename(msg)
	}

	if m.screen == screenWorkflowForm {
		return m.updateWorkflowForm(msg)
	}

	if m.screen == screenWorkflowDelete {
		return m.updateWorkflowDelete(msg)
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.screen == screenSearch {
		return m.updateSearch(key)
	}

	if m.screen == screenChats {
		return m.updateChats(key)
	}

	return m, nil
}

func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && bound(key.String(), m.keys.Form.Submit) {
		return m.submitSession()
	}

	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		return m.submitSession()
	case huh.StateAborted:
		m.newChatWorkflow = nil
		m.screen = screenChats
		m.chatCursor = clampCursor(m.chatCursor, len(m.chatRows()))
		m.stageSelection()
		return m, nil
	default:
		return m, cmd
	}
}

func (m model) submitSession() (tea.Model, tea.Cmd) {
	var workflowID *string
	if m.newChatWorkflow != nil {
		id := m.newChatWorkflow.ID
		workflowID = &id
	}

	m.manager.NewSession(agents.NewSessionParams{
		Client:     m.manager.InvokingClient(),
		Title:      m.form.GetString("title"),
		Dir:        m.form.GetString("dir"),
		Flags:      m.form.GetString("flags"),
		Prompt:     m.form.GetString("prompt"),
		WorkflowID: workflowID,
	})
	m.newChatWorkflow = nil
	m.reload()
	m.screen = screenChats
	m.chatCursor = clampCursor(m.chatCursor, len(m.chatRows()))
	m.stageSelection()
	return m, nil
}
