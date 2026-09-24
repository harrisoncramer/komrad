package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"weave/config"
	"weave/internal/browser"
	"weave/internal/engine"
	"weave/internal/panes"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/hook/v1/hookv1connect"
	schedulev1connect "weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	tea "charm.land/bubbletea/v2"
)

const panelViewer = "/dev/ttys004"

// Panel is the panel's model driven the way the running program drives it.
type Panel struct {
	model model
	post  *postbox
	tmux  *recordingTmux
}

type unsplittableTmux struct {
	*recordingTmux
}

type recordingTmux struct {
	resizes     []string
	path        string
	swaps       int
	windowSizes map[string]string
	sessionOpts map[string]string
	respawns    []string
	width       int
	height      int
	// paneWidths is one pane's own width, falling back to width where a pane has no entry.
	paneWidths    map[string]int
	windowWidth   int
	sentKeys      []string
	detaches      int
	killed        []string
	belowAnchors  []string
	globals       map[string]string
	infoPane      string
	selected      []string
	respawnPanes  []string
	respawnDirs   []string
	deadPanes     map[string]bool
	sessionClient string
	zoomed        bool
	// keysElsewhere is the keyboard sitting in a pane other than the one being asked about.
	keysElsewhere bool
}

// HarnessFrameReader answers whether a pane is drawing its harness yet.
type HarnessFrameReader = harnessFrameReader

// NewPanelParams is what a panel under test is built over.
type NewPanelParams struct {
	Agents        agentv1connect.AgentServiceClient
	Workflows     workflowv1connect.WorkflowServiceClient
	Schedules     schedulev1connect.ScheduleServiceClient
	Hooks         hookv1connect.HookServiceClient
	Worktrees     worktreev1connect.WorktreeServiceClient
	Git           ReviewGit
	Keys          config.Keybindings
	DetailsHeight float64
	SidebarSide   string
	Width         int
	Height        int
	Settings      PanelSettings
	Engine        engine.Remote
	HarnessFrames harnessFrameReader
	ConfigErr     error
	StartupErr    error
	RefuseSplits  bool
	Binary        BinaryWatcher
	Browser       browser.Opener
}

// NewPanel builds the panel the way weave panel does, over a recording tmux.
func NewPanel(params NewPanelParams) *Panel {
	settings := params.Settings
	settings.Pane = "%0"
	tmux := &recordingTmux{
		width:       params.Width,
		height:      params.Height,
		windowWidth: params.Width,
	}
	var driven uiTmux = tmux
	if params.RefuseSplits {
		driven = unsplittableTmux{tmux}
	}
	post := newPostbox()
	layout := config.DefaultLayout()
	if params.DetailsHeight != 0 {
		layout.DetailsHeight = params.DetailsHeight
	}
	if params.SidebarSide != "" {
		layout.SidebarSide = params.SidebarSide
	}

	return &Panel{
		model: newModel(Deps{
			PopupWindow:   NewPopupWindow(tmux, "test-session"),
			Tmux:          driven,
			Git:           params.Git,
			Agents:        params.Agents,
			Workflows:     params.Workflows,
			Schedules:     params.Schedules,
			Hooks:         params.Hooks,
			Worktrees:     params.Worktrees,
			Keys:          params.Keys,
			Layout:        layout,
			Engine:        params.Engine,
			HarnessFrames: params.HarnessFrames,
			Settings:      settings,
			ConfigErr:     params.ConfigErr,
			StartupErr:    params.StartupErr,
			Binary:        params.Binary,
			Browser:       params.Browser,
		}, true, post),
		post: post,
		tmux: tmux,
	}
}

// Init is the command the program runs as it starts.
func (p *Panel) Init() tea.Cmd {
	return p.model.Init()
}

// Update hands the panel one message.
func (p *Panel) Update(msg tea.Msg) tea.Cmd {
	next, cmd := p.model.Update(msg)
	if updated, ok := next.(model); ok {
		p.model = updated
	}

	size := p.post.takePending()
	if size == nil {
		return cmd
	}

	return tea.Batch(cmd, func() tea.Msg {
		return *size
	})
}

// View is the frame the panel draws.
func (p *Panel) View() string {
	return p.model.render()
}

// NewKeyPress is the press bubbletea delivers for a key as a binding spells it.
func NewKeyPress(key string) tea.KeyPressMsg {
	return keyMsg(key)
}

// IsZoomed reports whether the panel last asked tmux to zoom its pane over the window.
func (p *Panel) IsZoomed() bool {
	return p.tmux.zoomed
}

// CountRespawns is how many panes the panel has respawned.
func (p *Panel) CountRespawns() int {
	return len(p.tmux.respawns)
}

// CountSentKeys is how many key sends the panel has typed into panes.
func (p *Panel) CountSentKeys() int {
	return len(p.tmux.sentKeys)
}

// NewWatchLost is the message the panel hears when its watch stream ends.
func NewWatchLost(err error) tea.Msg {
	return watchLostMsg{err: err}
}

// ReadGuestPane is the pane the panel has swapped into its stage.
func (p *Panel) ReadGuestPane() string {
	return p.model.frame.GuestPane()
}

// ReadSelectedPanes is every pane the panel has handed the keyboard to.
func (p *Panel) ReadSelectedPanes() []string {
	return p.tmux.selected
}

// ReadRespawns is every command the panel has respawned a pane with.
func (p *Panel) ReadRespawns() []string {
	return p.tmux.respawns
}

// ReadKilledPanes is every pane the panel has killed.
func (p *Panel) ReadKilledPanes() []string {
	return p.tmux.killed
}

// SetPaneWidth makes tmux report a pane at the given width.
func (p *Panel) SetPaneWidth(pane string, width int) {
	if p.tmux.paneWidths == nil {
		p.tmux.paneWidths = map[string]int{}
	}

	p.tmux.paneWidths[pane] = width
}

// SetWindowWidth makes tmux report the window at the given width.
func (p *Panel) SetWindowWidth(width int) {
	p.tmux.windowWidth = width
}

// SetKeysElsewhere makes tmux report whether the keyboard is in a pane other than the panel's.
func (p *Panel) SetKeysElsewhere(elsewhere bool) {
	p.tmux.keysElsewhere = elsewhere
}

// ReadResizes is every pane resize the panel has asked tmux for.
func (p *Panel) ReadResizes() []string {
	return p.tmux.resizes
}

// ReadWindowSizes is the size the panel last gave each agent's window.
func (p *Panel) ReadWindowSizes() map[string]string {
	return p.tmux.windowSizes
}

// SetPaneSize makes tmux report every pane and the window at the given size.
func (p *Panel) SetPaneSize(width, height int) {
	p.tmux.width, p.tmux.height, p.tmux.windowWidth = width, height, width
}

func (unsplittableTmux) SplitRight(string, string, string) string {
	return ""
}

func (unsplittableTmux) SplitLeft(string, string, string) string {
	return ""
}

func (t *recordingTmux) ShowGlobal(name string) string {
	return t.globals[name]
}

func (t *recordingTmux) GetSessionClient(string) string {
	return t.sessionClient
}

func (t *recordingTmux) SetSessionOption(_, name, value string) {
	if t.sessionOpts == nil {
		t.sessionOpts = map[string]string{}
	}

	t.sessionOpts[name] = value
}

func (t *recordingTmux) DetachSession(string) {
	t.detaches++
}

func (t *recordingTmux) BindKey(string, ...string) {}

func (t *recordingTmux) BindRootKey(string, ...string) {}

func (t *recordingTmux) UnbindRootKey(string) {}

func (t *recordingTmux) UnbindKey(string) {}

func (t *recordingTmux) GetHostPath() string {
	return t.path
}

func (t *recordingTmux) GetDefaultShell() string {
	return "/bin/zsh"
}

func (t *recordingTmux) SendKeys(target string, keys ...string) error {
	t.sentKeys = append(t.sentKeys, strings.Join(append([]string{target}, keys...), " "))

	return nil
}

// MovePanes replays a change's pane moves onto the fake, so a test asserting on swaps and
// zooms sees what the frame asked for whether or not they were collected first.
func (t *recordingTmux) MovePanes(moves []panes.Move) {
	for _, move := range moves {
		switch move.Kind {
		case panes.KindZoom:
			t.SetPaneZoomed(move.Pane, move.Zoomed)
		case panes.KindSwap:
			t.SwapPanes(move.Pane, move.Other)
		case panes.KindRespawn:
			t.RespawnPane(move.Pane, move.Dir, move.Argv...)
		case panes.KindResize:
			t.ResizePane(move.Pane, strconv.Itoa(move.Width))
		}
	}
}

func (t *recordingTmux) IsPaneAlive(pane string) bool {
	return !t.deadPanes[pane]
}

func (t *recordingTmux) SetPaneZoomed(_ string, on bool) {
	t.zoomed = on
}

func (t *recordingTmux) GetWindowWidth(string) int {
	return t.windowWidth
}

func (t *recordingTmux) GetPaneWidth(pane string) int {
	if width, ok := t.paneWidths[pane]; ok {
		return width
	}

	return t.width
}

// GetPaneFocus answers the way tmux was measured to: a pane the keys have moved off is neither
// the active one nor zoomed any more.
func (t *recordingTmux) GetPaneFocus(string) (focused, zoomed bool) {
	if t.keysElsewhere {
		return false, false
	}

	return true, t.zoomed
}

func (t *recordingTmux) GetPaneHeight(string) int {
	return t.height
}

func (t *recordingTmux) SplitRight(string, string, string) string {
	return t.split("%1")
}

func (t *recordingTmux) SplitLeft(string, string, string) string {
	return t.split("%1")
}

func (t *recordingTmux) SplitBelow(anchor, _, _ string) string {
	t.belowAnchors = append(t.belowAnchors, anchor)

	return t.split("%2")
}

func (t *recordingTmux) SplitAbove(anchor, _, _ string) string {
	t.belowAnchors = append(t.belowAnchors, anchor)

	return t.split("%2")
}

// split answers with the pane a split left, dropping the zoom the way tmux does.
func (t *recordingTmux) split(pane string) string {
	t.zoomed = false

	return pane
}

func (t *recordingTmux) SetInfoPane(pane string) {
	t.infoPane = pane
}

func (t *recordingTmux) SelectPane(pane string) {
	t.selected = append(t.selected, pane)
}

func (t *recordingTmux) SetGlobal(name, value string) {
	if t.globals == nil {
		t.globals = map[string]string{}
	}

	t.globals[name] = value
}

func (t *recordingTmux) GetSiblingPanes(string) []string {
	return nil
}

func (t *recordingTmux) GetPaneOwner(string) string {
	return ""
}

// SwapPanes, BreakPane, KillPane and ResizePane each drop the zoom, the way tmux does.
func (t *recordingTmux) SwapPanes(string, string) {
	t.swaps++
	t.zoomed = false
}

func (t *recordingTmux) BreakPane(string) {
	t.zoomed = false
}

func (t *recordingTmux) KillPane(pane string) {
	t.killed = append(t.killed, pane)
	t.zoomed = false
}

func (t *recordingTmux) NewWindow(string, string, string) (string, error) {
	return "%2", nil
}

// ResizeWindow records the size an agent's own window was put at, keyed by pane.
func (t *recordingTmux) ResizeWindow(pane string, width, height int) {
	if t.windowSizes == nil {
		t.windowSizes = map[string]string{}
	}

	t.windowSizes[pane] = fmt.Sprintf("%dx%d", width, height)
}

func (t *recordingTmux) IsPaneRunningCLI(pane string) bool {
	return pane != ""
}

func (t *recordingTmux) ResizePane(pane, width string) {
	t.zoomed = false
	t.resizes = append(t.resizes, pane+" "+width)
}

func (t *recordingTmux) RespawnPane(pane, dir string, argv ...string) {
	t.respawns = append(t.respawns, strings.Join(argv, " "))
	t.respawnPanes = append(t.respawnPanes, pane)
	t.respawnDirs = append(t.respawnDirs, dir)
}

// NewTick is the refresh loop's tick as it lands at the given time.
func NewTick(at time.Time) tea.Msg {
	return tickMsg{at: at}
}

// SetShown makes tmux report whether a client is looking at the panel's session.
func (p *Panel) SetShown(isShown bool) {
	p.tmux.sessionClient = ""
	if isShown {
		p.tmux.sessionClient = panelViewer
	}
}
