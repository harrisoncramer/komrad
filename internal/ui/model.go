// Package ui is the bubbletea panel for browsing and acting on Claude agents.
package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"claude-agents/internal/appconfig"
	"claude-agents/internal/client"
	"claude-agents/internal/errui"
	agentv1 "claude-agents/proto/gen/agent/v1"
	"claude-agents/proto/gen/agent/v1/agentv1connect"
	schedulev1connect "claude-agents/proto/gen/schedule/v1/schedulev1connect"
	workflowv1 "claude-agents/proto/gen/workflow/v1"
	"claude-agents/proto/gen/workflow/v1/workflowv1connect"

	agentstore "claude-agents/internal/store/agent"
	schedulestore "claude-agents/internal/store/schedule"
	workflowstore "claude-agents/internal/store/workflow"

	"connectrpc.com/connect"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// refreshInterval is how often the panel re-lists over RPC. It runs off the
// render loop, so a slow serve can never freeze the UI.
const (
	refreshInterval = time.Second
	agentsTimeout   = 2 * time.Second
)

type refreshTickMsg struct{}

// listsLoadedMsg carries the result of an async re-list. Both lists come back
// together, since a workflow edited anywhere other than this panel's own form is
// invisible until the listing is read again. A non-nil err means the service could
// not be reached, which the panel shows rather than absorbs. The rows it is holding
// are from the last successful poll, and left on screen they would read as the
// current truth.
type listsLoadedMsg struct {
	agents    []agentItem
	workflows []workflowstore.Workflow
	schedules []scheduleItem
	err       error
}

func refreshTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return refreshTickMsg{} })
}

// loadListsCmd re-lists the agents and the workflows off the UI goroutine with a
// timeout, so the render loop never blocks on the RPCs or the per-agent tmux
// liveness checks. Both share one deadline, since together they are one refresh.
func (m model) loadListsCmd() tea.Cmd {
	agents, workflows, schedules := m.api.agents, m.api.workflows, m.api.schedules

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
		defer cancel()

		listed, err := client.ListAllAgents(ctx, agents)
		if err != nil {
			return listsLoadedMsg{err: err}
		}

		listedWorkflows, err := client.ListAllWorkflows(ctx, workflows)
		if err != nil {
			return listsLoadedMsg{err: err}
		}

		listedSchedules, err := client.ListAllSchedules(ctx, schedules)
		if err != nil {
			return listsLoadedMsg{err: err}
		}

		return listsLoadedMsg{
			agents:    agentItemsFrom(listed),
			workflows: workflowsFrom(listedWorkflows),
			schedules: scheduleItemsFrom(listedSchedules),
		}
	}
}

// agentItemsFrom maps listed agents to agent items, marking which panes are live.
func agentItemsFrom(listed []*agentv1.Agent) []agentItem {
	items := make([]agentItem, 0, len(listed))
	for _, agent := range listed {
		items = append(items, agentItem{agent: protoToAgent(agent), alive: agent.GetAlive()})
	}

	return items
}

// Run launches the panel and blocks for the life of the persistent manager tmux
// session. Agents are read through the ConnectRPC client. The panel is the nav
// pane, the resource it hovers is shown in a sibling stage pane, and focus moves
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
	// dimStyle is shared with the error screens rather than restated here, so the
	// panel's secondary text and theirs cannot drift to different greys.
	dimStyle = errui.Dim
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
	screenSchedules
	screenScheduleForm
	screenScheduleDelete
)

// actionTimeout bounds a single call made in response to a keypress. It is
// generous because creating an agent opens a tmux window on the far side, and it
// exists so a wedged server leaves the panel usable rather than frozen.
const actionTimeout = 10 * time.Second

// uiTmux is what the panel needs from tmux, enough to lay out its own panes plus
// the working directory of the client looking at it.
type uiTmux interface {
	stageTmux
	ClientPath(client string) string
}

// api holds the service clients the panel acts through. Every change to an agent or
// a workflow goes over these, the same way the MCP tools and the CLI reach them, so
// what the panel may do and what a model may do cannot drift apart.
type api struct {
	agents    agentv1connect.AgentServiceClient
	workflows workflowv1connect.WorkflowServiceClient
	schedules schedulev1connect.ScheduleServiceClient
}

// Deps are what the panel needs to run. The two services it acts through, the
// popup chrome it hides itself with, and a tmux client for its own layout.
type Deps struct {
	Popup     *Popup
	Tmux      uiTmux
	Agents    agentv1connect.AgentServiceClient
	Workflows workflowv1connect.WorkflowServiceClient
	Schedules schedulev1connect.ScheduleServiceClient
	Keys      appconfig.Keybindings
	// Addr is where Client points, shown on the error screen when the service
	// stops answering so the address to go looking at is on screen.
	Addr string
	// ConfigErr is set when the user's config file would not parse. The panel shows
	// it on its error screen instead of starting, so the message is readable rather
	// than written to a popup that closes on top of it. Keys fall back to the
	// built-in defaults, which is what the panel would have used anyway.
	ConfigErr error
	// StartupErr is set when the server the panel talks through could not be
	// started, most often because its database is not up. It is shown on the error
	// screen for the same reason ConfigErr is: this process is the command of the
	// tmux session behind the popup, so exiting takes the message down with it.
	StartupErr error
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
	// stage is the preview pane beside the list. It is the panel's own layout, so it
	// drives tmux directly. It fires on every cursor move, and a round trip per
	// keypress would be felt.
	stage *Stage

	// addr is where the service is expected to answer, shown when it does not.
	addr string
	// unreachable is the error from the last failed poll, nil while the service is
	// answering. The poll keeps running while it is set, so the panel recovers on
	// its own once the service is back.
	unreachable error
	// configErr is set when the config file would not parse. Unlike unreachable it
	// never clears, since the file is read once at startup, so the screen it shows
	// is the whole life of the process.
	configErr error
	// startupErr is set when the server could not be started. It never clears
	// either: nothing tries a second time, so a panel that came up without a server
	// has none for as long as it runs.
	startupErr error
	// actionErr is the last write the service refused, shown as a banner over the
	// screen it happened on. It is cleared when the next action is attempted and by
	// esc, never by a poll: a refused delete is still worth reading a second later.
	actionErr *errui.Banner

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

	// Schedules screen state. The list, which row is hovered, and the schedules
	// the form and the delete prompt are working on, both nil when creating one or
	// when neither is open.
	schedules            []scheduleItem
	scheduleCursor       int
	scheduleEditTarget   *schedulestore.Schedule
	scheduleDeleteTarget *schedulestore.Schedule

	// Search screen state. The live query, its hits, and which hit is hovered.
	searchQuery  string
	searchHits   []searchHit
	searchCursor int

	keys appconfig.Keybindings
}

func newModel(deps Deps) model {
	m := model{
		api:        api{agents: deps.Agents, workflows: deps.Workflows, schedules: deps.Schedules},
		popup:      deps.Popup,
		tmux:       deps.Tmux,
		stage:      NewStage(deps.Tmux),
		addr:       deps.Addr,
		keys:       deps.Keys,
		configErr:  deps.ConfigErr,
		startupErr: deps.StartupErr,
		screen:     screenAgents,
	}

	// A failure that came up with the process shows its error in place of the panel,
	// so none of the panel's setup runs: no stage pane is split and nothing is
	// listed. The screen is all this process does.
	if m.fatal() != nil {
		return m
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
// blank, meaning "where I am", and only the panel can answer that, since it is the
// invoking tmux client's path. The API refuses a blank directory because a server
// has no way to know which pane the user is looking at.
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
func (m model) renameAgent(agent agentstore.Agent, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || name == agent.Name {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := m.api.agents.UpdateAgent(ctx, connect.NewRequest(&agentv1.UpdateAgentRequest{
		Id:   agent.ID,
		Name: name,
	}))

	return err
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

	return workflowsFrom(listed), nil
}

// workflowsFrom renders listed workflows into the store type the tree is written
// against.
func workflowsFrom(listed []*workflowv1.Workflow) []workflowstore.Workflow {
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

	return workflows
}

// reloadWorkflows refreshes the workflow list backing the tree's folder headers,
// clamping the cursor into the new length.
func (m *model) reloadWorkflows() {
	workflows, err := m.listWorkflows()
	if err != nil {
		// The tree already on screen is kept. Blanking it would say every workflow
		// had been deleted, which is not what a list that failed to answer means.
		m.failAction("Could not list workflows", err)

		return
	}

	m.workflows = workflows
	m.workflowCursor = clampCursor(m.workflowCursor, len(workflows))
}

// applyWorkflows merges a freshly-listed set into the on-screen workflows. The
// listing sorts by recency, so taking it wholesale on every refresh would reshuffle
// the tree's folders under the cursor the moment any workflow was touched. The
// current order is kept, new workflows are appended, vanished ones are dropped, and
// the ones that remain take their fresh fields, so a context edited through the MCP
// tools or another panel shows here without anything moving.
func (m *model) applyWorkflows(fresh []workflowstore.Workflow) {
	byID := make(map[string]workflowstore.Workflow, len(fresh))
	for _, w := range fresh {
		byID[w.ID] = w
	}

	seen := make(map[string]bool, len(m.workflows))
	workflows := make([]workflowstore.Workflow, 0, len(fresh))
	for _, cur := range m.workflows {
		if w, ok := byID[cur.ID]; ok {
			seen[cur.ID] = true
			workflows = append(workflows, w)
		}
	}
	for _, w := range fresh {
		if !seen[w.ID] {
			workflows = append(workflows, w)
		}
	}

	m.workflows = workflows
	m.workflowCursor = clampCursor(m.workflowCursor, len(workflows))
}

// applyAgents merges a freshly-listed set into the on-screen agents. It keeps the
// current order rather than re-sorting by recency, since reordering under the
// cursor is jarring and defeats an index-based selection. New agents are appended
// and vanished ones dropped, and the cursor stays on whichever agent it was on. The
// fresh items carry updated state and liveness, so a reconciler change shows
// without a structural reload.
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
		WorkflowID:      s.GetWorkflowId(),
		ScheduleID:      s.ScheduleId,
		CreatedAt:       created,
	}
}

// errNoWorkflow is what the panel reports when a create is attempted with no
// workflow to put the agent in, which only happens when none exist at all.
var errNoWorkflow = errors.New("every agent belongs to a workflow, so create one first")

// openNewAgent opens the new-agent form, seeded from the given workflow's
// blueprint, and records that workflow so the created agent is placed in it. A
// nil workflow means there are none to create an agent in, which is reported
// rather than opening a form whose submit could not go anywhere.
func (m model) openNewAgent(workflow *workflowstore.Workflow) (tea.Model, tea.Cmd) {
	if workflow == nil {
		m.failAction("Could not start an agent", errNoWorkflow)

		return m, nil
	}

	m.newAgentWorkflow = workflow
	m.form = newAgentForm(m.keys.Form, workflow.Cwd, workflow.Flags)
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
		m.report(
			fmt.Sprintf("Could not rename agent %q", m.renameTarget.Name),
			m.renameAgent(m.renameTarget, m.form.GetString("name")),
		)
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

// newAgentForm builds the new-agent form. defaultDir and defaultFlags prefill the
// directory and flags fields from the enclosing workflow's config, so an agent
// created inside a workflow starts from its blueprint. Both are blank at the
// root.
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

// onFormScreen reports whether a form owns the input, which decides whether a key
// belongs to the form or to the panel's own chrome.
func (m model) onFormScreen() bool {
	switch m.screen {
	case screenForm, screenRename, screenWorkflowForm, screenWorkflowDelete,
		screenScheduleForm, screenScheduleDelete:
		return true
	default:
		return false
	}
}

// fatal is the failure that replaces the panel, or nil when there is none. Both
// are settled before the panel starts and neither can clear itself, so either one
// means there is no panel behind the screen it shows. A config that would not parse
// comes first: a panel that never read its keybindings never got as far as trying
// to start a server.
func (m model) fatal() error {
	if m.configErr != nil {
		return m.configErr
	}

	return m.startupErr
}

// Init starts the async agents refresh so the list's counts and states stay current.
// A startup failure means there is no list to refresh, so nothing is started.
func (m model) Init() tea.Cmd {
	if m.fatal() != nil {
		return nil
	}

	return tea.Batch(refreshTick(), m.loadListsCmd())
}

// Update routes messages and key input to the active screen.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// A startup failure screen is terminal: there is nothing to act on, so any key
	// dismisses it. Quitting exits zero, which is what lets the popup close on the
	// keypress rather than lingering.
	if m.fatal() != nil {
		if _, ok := msg.(tea.KeyMsg); ok {
			return m, tea.Quit
		}

		return m, nil
	}

	// The banner is chrome rather than a screen, so it is dismissed with esc as a
	// literal rather than through the rebindable back binding: back also carries
	// left and h, and navigation keys should move the cursor, not clear an error.
	// Form screens keep their own esc, which aborts the form.
	if m.actionErr != nil && !m.onFormScreen() {
		if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" {
			m.actionErr = nil

			return m, nil
		}
	}

	if _, ok := msg.(refreshTickMsg); ok {
		return m, tea.Batch(m.loadListsCmd(), refreshTick())
	}

	if loaded, ok := msg.(listsLoadedMsg); ok {
		m.unreachable = loaded.err
		if loaded.err == nil {
			// Workflows first, since the tree's rows and so the cursor the agents are
			// reapplied against are built over them.
			m.applyWorkflows(loaded.workflows)
			m.applyAgents(loaded.agents)
			m.applySchedules(loaded.schedules)

			// A refresh can change what the cursor is sitting on without the cursor
			// moving, so the stage is redrawn from the merged lists. The stage is a
			// no-op when what it already shows has not changed, which is what keeps
			// this from respawning its pane every tick. Only the agents screen owns
			// the stage: a search preview or a form's stage is not the tree's to
			// take back while it is up.
			if m.screen == screenAgents {
				m.stageSelection()
			}
			if m.screen == screenSchedules {
				m.stageSchedule()
			}
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

	if m.screen == screenScheduleForm {
		return m.updateScheduleForm(msg)
	}

	if m.screen == screenScheduleDelete {
		return m.updateScheduleDelete(msg)
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.screen == screenSearch {
		return m.updateSearch(key)
	}

	if m.screen == screenSchedules {
		return m.updateSchedules(key)
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
	// The form is only reachable from inside a workflow, so this holds one by the
	// time a submit happens. It is checked rather than dereferenced because a
	// create with no workflow would be refused by the service anyway, and saying
	// so here costs a round trip less.
	if m.newAgentWorkflow == nil {
		m.failAction("Could not start the agent", errNoWorkflow)
		m.screen = screenAgents
		m.stageSelection()

		return m, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	// A launch failure leaves the list unchanged, so it has to be reported. The
	// refresh below would otherwise be the only signal, and a list that comes back
	// without the new agent reads the same as a form that never submitted.
	_, err := m.api.agents.CreateAgent(ctx, connect.NewRequest(&agentv1.CreateAgentRequest{
		Prompt:     m.form.GetString("prompt"),
		Title:      m.form.GetString("title"),
		Dir:        m.currentDir(m.form.GetString("dir")),
		Flags:      m.form.GetString("flags"),
		WorkflowId: m.newAgentWorkflow.ID,
	}))
	m.report("Could not start the agent", err)
	m.newAgentWorkflow = nil
	m.reload()
	m.screen = screenAgents
	m.agentCursor = clampCursor(m.agentCursor, len(m.agentRows()))
	m.stageSelection()
	return m, nil
}
