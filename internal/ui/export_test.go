package ui

import (
	"weave/config"
	"weave/internal/engine"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/hook/v1/hookv1connect"
	schedulev1connect "weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"
	"weave/proto/gen/worktree/v1/worktreev1connect"

	tea "charm.land/bubbletea/v2"
)

// Panel is the panel's model driven the way the running program drives it.
type Panel struct {
	model model
	post  *postbox
	tmux  *recordingTmux
}

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
}

type unsplittableTmux struct {
	*recordingTmux
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

// MoveKeysElsewhere makes tmux report the keyboard in a pane other than the panel's.
func (p *Panel) MoveKeysElsewhere(elsewhere bool) {
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
