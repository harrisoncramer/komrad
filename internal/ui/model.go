// Package panel is the bubbletea UI for browsing and acting on Claude agents.
package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"claude-agents/internal/appconfig"
	"claude-agents/internal/client"
	agentv1 "claude-agents/proto/gen/agent/v1"
	"claude-agents/proto/gen/agent/v1/agentv1connect"
	"claude-agents/proto/gen/workflow/v1/workflowv1connect"

	agentstore "claude-agents/internal/store/agent"
	workflowstore "claude-agents/internal/store/workflow"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// agentsInterval is how often the agents list re-lists over RPC. It runs off the
// render loop, so a slow serve can never freeze the UI.
const (
	agentsInterval = time.Second
	agentsTimeout  = 2 * time.Second
)

type agentsTickMsg struct{}

// agentsLoadedMsg carries the result of an async re-list. A non-nil err means the
// service could not be reached, which the panel shows rather than absorbs: the
// rows it is holding are from the last successful poll, and left on screen they
// read as the current truth when they are not.
type agentsLoadedMsg struct {
	items []agentItem
	err   error
}

func agentsTick() tea.Cmd {
	return tea.Tick(agentsInterval, func(time.Time) tea.Msg { return agentsTickMsg{} })
}

// loadAgentsCmd re-lists the sessions off the UI goroutine with a timeout, so the
// render loop never blocks on the RPC or the per-agent tmux liveness checks.
func (m model) loadAgentsCmd() tea.Cmd {
	sessions := m.api.agents

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
		defer cancel()

		listed, err := client.ListAllAgents(ctx, sessions)
		if err != nil {
			return agentsLoadedMsg{err: err}
		}

		return agentsLoadedMsg{items: agentItemsFrom(listed)}
	}
}

// agentItemsFrom maps listed sessions to agent items, marking which panes are live.
func agentItemsFrom(sessions []*agentv1.Agent) []agentItem {
	items := make([]agentItem, 0, len(sessions))
	for _, session := range sessions {
		items = append(items, agentItem{agent: protoToAgent(session), alive: session.GetAlive()})
	}

	return items
}

// Run launches the panel and blocks for the life of the persistent manager
// session. Sessions are read through the ConnectRPC client. The panel is the nav
// pane. The resource it hovers is shown in a sibling stage pane, and focus moves
// between the two with the user's own tmux bindings.
func Run(deps Deps) error {
	if _, err := tea.NewProgram(newModel(deps), tea.WithAltScreen()).Run(); err != nil {
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
		m.popup.Hide()
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
	case agentstore.StateDone:
		return "✓"
	case agentstore.StateWaiting:
		return "!"
	case agentstore.StateThinking:
		return "…"
	default:
		return "•"
	}
}

func stateColor(state string) lipgloss.Color {
	switch state {
	case agentstore.StateDone:
		return lipgloss.Color("#98BB6C")
	case agentstore.StateWaiting:
		return lipgloss.Color("#E82424")
	case agentstore.StateThinking:
		return lipgloss.Color("#FF9E3B")
	default:
		return lipgloss.Color("#7E9CD8")
	}
}

type screen int

const (
	screenAgents screen = iota
	screenForm
	screenRename
	screenWorkflowForm
	screenWorkflowDelete
	screenSearch
)

// actionTimeout bounds a single call made in response to a keypress. It is
// generous because creating an agent opens a tmux window on the far side, and it
// exists so a wedged server leaves the panel usable rather than frozen.
const actionTimeout = 10 * time.Second

// uiTmux is what the panel needs from tmux: enough to lay out its own panes, and
// the working directory of the client looking at it.
type uiTmux interface {
	stageTmux
	ClientPath(client string) string
}

// api holds the service clients the panel acts through. Every change to an agent
// or a workflow goes over these, the same way the MCP tools and the CLI reach
// them, so what the panel may do and what a model may do cannot drift apart.
type api struct {
	agents    agentv1connect.AgentServiceClient
	workflows workflowv1connect.WorkflowServiceClient
}

// Deps are what the panel needs to run: the two services it acts through, the
// popup chrome it hides itself with, a tmux client for its own layout, and the
// RPC client it lists through.
type Deps struct {
	Popup     *Popup
	Tmux      uiTmux
	Agents    agentv1connect.AgentServiceClient
	Workflows workflowv1connect.WorkflowServiceClient
	Keys      appconfig.Keybindings
	// Addr is where Client points, shown on the error screen when the service
	// stops answering so the address to go looking at is on screen.
	Addr string
}

type model struct {
	popup *Popup
	// api is how the panel reads and changes anything. It is grouped so the
	// clients do not collide with the lists they populate.
	api api
	// tmux lays out the panel's own panes and answers which directory the user is
	// looking at, which is the one thing about a new agent the service cannot work
	// out for itself.
	tmux uiTmux
	// stage is the preview pane beside the list. It is the panel's own layout, so
	// it drives tmux directly: it fires on every cursor move, and a round trip per
	// keypress would be felt.
	stage *Stage

	// addr is where the service is expected to answer, shown when it does not.
	addr string
	// unreachable is the error from the last failed poll, nil while the service is
	// answering. The poll keeps running while it is set, so the panel recovers on
	// its own once the service is back.
	unreachable error

	screen screen

	agents      []agentItem
	agentCursor int

	// infoPinned shows the hovered agent's stored record in the stage instead of the
	// agent itself. A live agent otherwise occupies the stage with its own pane, so
	// its cwd and flags have nowhere else to show. Moving the cursor or opening an
	// agent clears it, so the pinned record never outlives the row it describes.
	infoPinned bool

	// workflows backs the tree's folder headers. workflowCursor is which workflow a
	// header-hover action targets. workflowRenameTarget and workflowEditTarget are
	// the workflows being renamed or edited while the workflow form is open, both
	// nil when creating one. workflowDeleteTarget is the one the delete prompt is
	// confirming.
	workflows            []workflowstore.Workflow
	workflowCursor       int
	workflowRenameTarget *workflowstore.Workflow
	workflowEditTarget   *workflowstore.Workflow
	workflowDeleteTarget *workflowstore.Workflow

	// newAgentWorkflow is the workflow a pending new-agent form will place its agent
	// in, or nil for an agent that belongs to no workflow. Set when the form opens
	// and consumed on submit.
	newAgentWorkflow *workflowstore.Workflow

	form         *huh.Form
	renameTarget agentstore.Agent

	// Search screen state. The live query, its hits, and which hit is hovered.
	searchQuery  string
	searchHits   []searchHit
	searchCursor int

	keys appconfig.Keybindings
}

func newModel(deps Deps) model {
	m := model{
		api:    api{agents: deps.Agents, workflows: deps.Workflows},
		popup:  deps.Popup,
		stage:  NewStage(deps.Tmux),
		addr:   deps.Addr,
		keys:   deps.Keys,
		screen: screenAgents,
	}
	// Split off the stage pane beside the panel, then show whatever the initial
	// selection lands on.
	m.stage.Init()
	m.reload()
	m.agentCursor = firstAgentRowIndex(m.agentRows())
	m.stageSelection()

	return m
}

// agentItem is a stored agent plus whether its tmux pane is currently alive. A
// dormant agent has a dead pane but is kept, since it can be restored from its stored state.
type agentItem struct {
	agent agentstore.Agent
	alive bool
}

// reload lists the agents through the service, marking which are live, and
// refreshes the workflow list. Records are never deleted here, so agents
// survive tmux restarts.
func (m *model) reload() {
	m.reloadWorkflows()

	listed, err := client.ListAllAgents(context.Background(), m.api.agents)
	if err != nil {
		m.agents = nil
		return
	}

	m.agents = agentItemsFrom(listed)
}

// currentDir fills in the directory a new agent starts in. The form allows a
// blank, meaning "where I am", and only the panel can answer that: it is the
// invoking tmux client's path. The API refuses a blank directory precisely
// because a server has no way to know which pane the user is looking at.
func (m model) currentDir(dir string) string {
	if trimmed := strings.TrimSpace(dir); trimmed != "" {
		return trimmed
	}
	if path := m.tmux.ClientPath(m.popup.InvokingClient()); path != "" {
		return path
	}

	home, _ := os.UserHomeDir()

	return home
}

// renameAgent renames an agent by id. A blank or unchanged name is a no-op, which
// keeps the form's empty submit from asking the service to do nothing.
func (m model) renameAgent(agent agentstore.Agent, name string) {
	name = strings.TrimSpace(name)
	if name == "" || name == agent.Name {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, _ = m.api.agents.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
		Id:   agent.ID,
		Name: name,
	}))
}

// listWorkflows fetches every workflow and renders them into the store type the
// tree is written against.
func (m model) listWorkflows() ([]workflowstore.Workflow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
	defer cancel()

	listed, err := client.ListAllWorkflows(ctx, m.api.workflows)
	if err != nil {
		return nil, err
	}

	workflows := make([]workflowstore.Workflow, 0, len(listed))
	for _, w := range listed {
		workflows = append(workflows, workflowstore.Workflow{
			ID:      w.GetId(),
			Name:    w.GetName(),
			Context: w.GetContext(),
			Cwd:     w.GetCwd(),
			Flags:   w.GetFlags(),
		})
	}

	return workflows, nil
}

// reloadWorkflows refreshes the workflow list backing the tree's folder headers,
// clamping the cursor into the new length.
func (m *model) reloadWorkflows() {
	workflows, err := m.listWorkflows()
	if err != nil {
		m.workflows = nil
		m.workflowCursor = 0
		return
	}

	m.workflows = workflows
	m.workflowCursor = clampCursor(m.workflowCursor, len(workflows))
}

// applyAgents merges a freshly-listed set into the on-screen agents. It keeps the
// current order rather than re-sorting by recency, since reordering under the
// cursor is jarring and defeats an index-based selection. New agents are appended
// and vanished ones dropped, and the cursor stays on whichever agent it was on.
// The fresh items carry updated state and liveness, so a reconciler change
// shows without a structural reload.
func (m *model) applyAgents(fresh []agentItem) {
	byName := make(map[string]agentItem, len(fresh))
	order := make([]string, 0, len(fresh))
	for _, item := range fresh {
		byName[item.agent.Name] = item
		order = append(order, item.agent.Name)
	}

	selected := ""
	if item, ok := m.selectedAgent(); ok {
		selected = item.agent.Name
	}

	seen := make(map[string]bool, len(m.agents))
	items := make([]agentItem, 0, len(fresh))
	for _, cur := range m.agents {
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

	m.agents = items

	// The cursor indexes the tree of headers and agents, so keep it on the same
	// agent across the rebuild, or clamp it into the new tree when that agent is
	// gone.
	rows := m.agentRows()
	if idx := rowIndexOfAgentName(rows, selected); idx >= 0 {
		m.agentCursor = idx
	} else {
		m.agentCursor = clampCursor(m.agentCursor, len(rows))
	}
}

// indexOfAgent returns the position of the named agent, or 0 when it is not found,
// so the cursor lands on the first row.
func indexOfAgent(items []agentItem, name string) int {
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

func protoToAgent(s *agentv1.Agent) agentstore.Agent {
	created, _ := time.Parse(time.RFC3339, s.GetCreatedAt())

	return agentstore.Agent{
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

// openNewAgent opens the new-agent form, seeded from the given workflow's
// blueprint when one is passed, and records that workflow so the created agent is
// placed in it. A nil workflow creates an agent that belongs to none.
func (m model) openNewAgent(workflow *workflowstore.Workflow) (tea.Model, tea.Cmd) {
	dir, flags := "", ""
	if workflow != nil {
		dir = workflow.Cwd
		flags = workflow.Flags
	}
	m.newAgentWorkflow = workflow
	m.form = newAgentForm(m.keys.Form, dir, flags)
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
		m.renameAgent(m.renameTarget, m.form.GetString("name"))
		m.reload()
		m.agentCursor = clampCursor(m.agentCursor, len(m.agentRows()))
		m.screen = screenAgents
		m.stageSelection()
		return m, nil
	case huh.StateAborted:
		m.screen = screenAgents
		m.stageSelection()
		return m, nil
	default:
		return m, cmd
	}
}

// newAgentForm builds the new-agent form. defaultDir and defaultFlags prefill
// the directory and flags fields from the enclosing workflow's config so an agent
// created inside a workflow starts from its blueprint. Both are blank at the root.
func newAgentForm(keys appconfig.FormKeys, defaultDir, defaultFlags string) *huh.Form {
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

// Init starts the async agents refresh so the list's counts and states stay current.
func (m model) Init() tea.Cmd {
	return tea.Batch(agentsTick(), m.loadAgentsCmd())
}

// Update routes messages and key input to the active screen.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(agentsTickMsg); ok {
		return m, tea.Batch(m.loadAgentsCmd(), agentsTick())
	}

	if loaded, ok := msg.(agentsLoadedMsg); ok {
		m.unreachable = loaded.err
		if loaded.err == nil {
			m.applyAgents(loaded.items)
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

	if m.screen == screenAgents {
		return m.updateAgents(key)
	}

	return m, nil
}

func (m model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && bound(key.String(), m.keys.Form.Submit) {
		return m.submitAgent()
	}

	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		return m.submitAgent()
	case huh.StateAborted:
		m.newAgentWorkflow = nil
		m.screen = screenAgents
		m.agentCursor = clampCursor(m.agentCursor, len(m.agentRows()))
		m.stageSelection()
		return m, nil
	default:
		return m, cmd
	}
}

func (m model) submitAgent() (tea.Model, tea.Cmd) {
	var workflowID *string
	if m.newAgentWorkflow != nil {
		id := m.newAgentWorkflow.ID
		workflowID = &id
	}

	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	// A launch failure leaves the list unchanged. The next refresh is the panel's
	// own report of what exists.
	_, _ = m.api.agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     m.form.GetString("prompt"),
		Title:      m.form.GetString("title"),
		Dir:        m.currentDir(m.form.GetString("dir")),
		Flags:      m.form.GetString("flags"),
		WorkflowId: workflowID,
	}))
	m.newAgentWorkflow = nil
	m.reload()
	m.screen = screenAgents
	m.agentCursor = clampCursor(m.agentCursor, len(m.agentRows()))
	m.stageSelection()
	return m, nil
}
