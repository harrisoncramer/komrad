// Package panel is the bubbletea UI for browsing and acting on Claude agents.
package panel

import (
	"context"
	"fmt"
	"time"

	"claude-agents/internal/agents"
	"claude-agents/internal/appconfig"
	"claude-agents/internal/search"
	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"
	"claude-agents/proto/gen/session/v1/sessionv1connect"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"google.golang.org/protobuf/types/known/emptypb"
)

// previewInterval is how often the chats preview re-captures the hovered pane.
// It is fast because the capture is a local tmux read. chatsInterval is how
// often the chats list re-lists over RPC. It is slower and, crucially, runs off
// the render loop, so a slow serve can never freeze the UI.
const (
	previewInterval = 200 * time.Millisecond
	chatsInterval   = time.Second
	chatsTimeout    = 2 * time.Second
)

type tickMsg struct{}
type chatsTickMsg struct{}

// chatsLoadedMsg carries the result of an async re-list. ok is false when the
// list failed, so the handler leaves the current chats in place rather than
// clobbering them to empty.
type chatsLoadedMsg struct {
	items []chatItem
	ok    bool
}

func tick() tea.Cmd {
	return tea.Tick(previewInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func chatsTick() tea.Cmd {
	return tea.Tick(chatsInterval, func(time.Time) tea.Msg { return chatsTickMsg{} })
}

// loadChatsCmd re-lists the sessions off the UI goroutine with a timeout, so the
// render loop never blocks on the RPC or the per-chat tmux liveness checks. When
// a workflow is in scope it lists only that workflow's chats.
func (m model) loadChatsCmd() tea.Cmd {
	sessions, manager := m.sessions, m.manager
	workflowID := ""
	if m.workflow != nil {
		workflowID = m.workflow.ID
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), chatsTimeout)
		defer cancel()

		listed, err := listSessions(ctx, sessions, workflowID)
		if err != nil {
			return chatsLoadedMsg{ok: false}
		}

		return chatsLoadedMsg{items: chatItemsFrom(listed, manager), ok: true}
	}
}

// listSessions lists chats through the service, scoped to a workflow when
// workflowID is set and every chat otherwise.
func listSessions(ctx context.Context, sessions sessionv1connect.SessionServiceClient, workflowID string) ([]*sessionv1.Session, error) {
	if workflowID != "" {
		resp, err := sessions.ListWorkflowSessions(ctx, connect.NewRequest(&sessionv1.ListWorkflowSessionsRequest{WorkflowId: workflowID}))
		if err != nil {
			return nil, fmt.Errorf("failed to list workflow sessions: %w", err)
		}

		return resp.Msg.GetSessions(), nil
	}

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

// Run launches the panel and blocks until it exits. Sessions are read through
// the ConnectRPC client. If the user opens an agent, Run returns it so the
// caller can attach in place, otherwise it returns nil. When the caller loops
// back after detaching from an agent it sets focus, and the panel opens on the
// chats picker with that agent selected instead of at the root list.
func Run(manager *agents.AgentManager, sessions sessionv1connect.SessionServiceClient, client string, keys appconfig.Keybindings, focus *store.Agent) (*store.Agent, error) {
	final, err := tea.NewProgram(newModel(manager, sessions, client, keys, focus), tea.WithAltScreen()).Run()
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
	screenList screen = iota
	screenChats
	screenSearch
	screenForm
	screenRename
	screenWorkflows
	screenWorkflowForm
	screenWorkflowDelete
)

type rowKind int

const (
	rowNewChat rowKind = iota
	rowChats
	rowWorkflows
	rowSearch
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

	// workflow is the current scope: nil at the root, or the workflow whose
	// chats, new-chat, and search are all filtered to it. workflows backs the
	// picker, and moveTarget is the chat being reassigned when the picker is
	// opened in move mode. workflowRenameTarget is the workflow being renamed
	// when the workflow form is open, nil when creating one.
	workflow             *store.Workflow
	workflows            []store.Workflow
	workflowCursor       int
	moveTarget           *store.Agent
	workflowRenameTarget *store.Workflow
	workflowDeleteTarget *store.Workflow

	searchInput   textinput.Model
	searchIndex   search.Index
	searchAgents  map[string]store.Agent
	searchResults []search.Result
	searchCursor  int
	searching     bool

	attach       *store.Agent
	form         *huh.Form
	renameTarget store.Agent

	keys appconfig.Keybindings

	width  int
	height int
}

func newModel(manager *agents.AgentManager, sessions sessionv1connect.SessionServiceClient, client string, keys appconfig.Keybindings, focus *store.Agent) model {
	input := textinput.New()
	input.Placeholder = "search chats by content"
	input.Prompt = "› "

	m := model{
		manager:     manager,
		sessions:    sessions,
		client:      client,
		keys:        keys,
		screen:      screenList,
		preview:     viewport.New(0, 0),
		searchInput: input,
	}
	m.reload()

	// When the caller loops back after detaching from an agent, open on the chats
	// picker with that agent selected instead of at the root list. If the agent
	// belongs to a workflow, restore that workflow scope first, so exiting the
	// chat lands back in its workflow's chats rather than the root list.
	if focus != nil {
		if focus.WorkflowID != nil {
			if workflow, err := manager.GetWorkflow(*focus.WorkflowID); err == nil && workflow != nil {
				m.workflow = workflow
				m.reload()
			}
		}

		for i, item := range m.chats {
			if item.agent.Name == focus.Name {
				m.screen = screenChats
				m.chatCursor = i
				break
			}
		}
	}

	return m
}

// chatItem is a stored agent plus whether its tmux pane is currently alive. A
// dormant agent has a dead pane but is kept, since it can be restored from its stored state.
type chatItem struct {
	agent store.Agent
	alive bool
}

// reload lists the in-scope agents through the service, marking which are live,
// and refreshes the workflow list. Records are never deleted here, so agents
// survive tmux restarts.
func (m *model) reload() {
	m.reloadWorkflows()

	workflowID := ""
	if m.workflow != nil {
		workflowID = m.workflow.ID
	}

	listed, err := listSessions(context.Background(), m.sessions, workflowID)
	if err != nil {
		m.chats = nil
		m.rows = m.makeRows()
		return
	}

	m.chats = chatItemsFrom(listed, m.manager)
	m.rows = m.makeRows()
}

// reloadWorkflows refreshes the workflow list backing the picker and the root
// row count, clamping the cursor into the new length.
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
	if m.chatCursor >= 0 && m.chatCursor < len(m.chats) {
		selected = m.chats[m.chatCursor].agent.Name
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
	m.rows = m.makeRows()
	m.chatCursor = clampCursor(indexOfChat(items, selected), len(items))

	// The chats screen has no valid selection with an empty list, and its key
	// handlers index m.chats directly. If an async refresh empties the list while
	// it is open (every agent vanished server-side), fall back to the root list
	// so a keypress cannot index out of range.
	if len(items) == 0 && m.screen == screenChats {
		m.screen = screenList
	}
}

// indexOfChat returns the position of the named chat, or 0 when it is not found
// (for example the selection was deleted), so the cursor lands on the first row.
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

// attachScoped returns the agent to attach to, with its workflow set to the
// view's current scope rather than the chat's stored workflow. This is what the
// caller loops back with, so back-navigation returns to the workflow the user
// was actually browsing: a chat opened from inside a workflow returns there,
// while one opened from the root list or a global search returns to the root,
// even if that chat happens to belong to a workflow.
func (m model) attachScoped(agent store.Agent) *store.Agent {
	if m.workflow != nil {
		id := m.workflow.ID
		agent.WorkflowID = &id
	} else {
		agent.WorkflowID = nil
	}

	return &agent
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

// makeRows builds the picker rows for the current scope. The workflows row shows
// only at the root, since the scoped view is itself inside one workflow and does
// not nest another workflow list.
func (m model) makeRows() []row {
	rows := []row{
		{kind: rowNewChat, label: "+  new chat"},
		{kind: rowChats, label: fmt.Sprintf("▸  chats (%d)", len(m.chats))},
	}
	if m.workflow == nil {
		rows = append(rows, row{kind: rowWorkflows, label: fmt.Sprintf("⧉  workflows (%d)", len(m.workflows))})
	}
	rows = append(rows, row{kind: rowSearch, label: "/  search"})

	return rows
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
		if m.chatCursor >= len(m.chats) {
			m.chatCursor = len(m.chats) - 1
		}
		m.screen = screenChats
		m.refreshPreview()
		return m, tick()
	case huh.StateAborted:
		m.screen = screenChats
		m.refreshPreview()
		return m, tick()
	default:
		return m, cmd
	}
}

func newSessionForm(keys appconfig.FormKeys) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewText().Key("prompt").Title("Initial prompt").Description("ctrl+e opens $EDITOR; shift+enter submits"),
			huh.NewInput().Key("title").Title("Title").Description("optional; claude names it if blank"),
			huh.NewInput().Key("dir").Title("Directory").Description("blank = current"),
			huh.NewInput().Key("flags").Title("Claude flags").Placeholder("--model opus"),
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

func (m model) Init() tea.Cmd {
	// Always drive the async chats refresh, so the root list's count and the
	// chats screen both stay current. Add the preview tick only on the chats
	// screen, where a hovered pane is shown.
	cmds := []tea.Cmd{chatsTick(), m.loadChatsCmd()}
	if m.screen == screenChats {
		cmds = append(cmds, tick())
	}

	return tea.Batch(cmds...)
}

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

	if _, ok := msg.(chatsTickMsg); ok {
		return m, tea.Batch(m.loadChatsCmd(), chatsTick())
	}

	if loaded, ok := msg.(chatsLoadedMsg); ok {
		if loaded.ok {
			m.applyChats(loaded.items)
		}

		return m, nil
	}

	if built, ok := msg.(indexBuiltMsg); ok {
		m.searchIndex = built.index
		m.searchAgents = built.agents
		m.searching = false
		m.runSearch()
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

	if m.screen == screenSearch {
		return m.updateSearch(msg)
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
	case screenWorkflows:
		return m.updateWorkflows(key)
	default:
		return m, nil
	}
}

func (m model) updateList(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	switch {
	case bound(k, m.keys.List.Quit):
		return m, tea.Quit
	case bound(k, m.keys.List.Back):
		// From a scoped list, back returns to the workflow picker rather than
		// quitting, so the workflow is a place the user steps in and out of.
		if m.workflow != nil {
			return m.exitWorkflowScope()
		}

		return m, tea.Quit
	case bound(k, m.keys.Nav.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case bound(k, m.keys.Nav.Down):
		if m.cursor < len(m.rows)-1 {
			m.cursor++
		}
	case bound(k, m.keys.List.Select):
		return m.selectRow(m.rows[m.cursor])
	case bound(k, m.keys.List.New):
		return m.selectRow(row{kind: rowNewChat})
	case bound(k, m.keys.List.Chats):
		return m.selectRow(row{kind: rowChats})
	case bound(k, m.keys.List.Workflows):
		if m.workflow == nil {
			return m.selectRow(row{kind: rowWorkflows})
		}
	case bound(k, m.keys.List.Search):
		return m.enterSearch()
	}

	return m, nil
}

func (m model) selectRow(selected row) (tea.Model, tea.Cmd) {
	switch selected.kind {
	case rowNewChat:
		m.form = newSessionForm(m.keys.Form)
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
	case rowWorkflows:
		m.moveTarget = nil
		return m.enterWorkflows()
	case rowSearch:
		return m.enterSearch()
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
		m.screen = screenList
		return m, nil
	default:
		return m, cmd
	}
}

func (m model) submitSession() (tea.Model, tea.Cmd) {
	var workflowID *string
	if m.workflow != nil {
		id := m.workflow.ID
		workflowID = &id
	}

	m.manager.NewSession(agents.NewSessionParams{
		Client:     m.client,
		Title:      m.form.GetString("title"),
		Dir:        m.form.GetString("dir"),
		Flags:      m.form.GetString("flags"),
		Prompt:     m.form.GetString("prompt"),
		WorkflowID: workflowID,
	})
	m.reload()
	m.screen = screenList
	return m, nil
}
