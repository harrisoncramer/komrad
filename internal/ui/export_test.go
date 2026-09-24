package ui

import (
	"weave/config"
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
	Agents    agentv1connect.AgentServiceClient
	Workflows workflowv1connect.WorkflowServiceClient
	Schedules schedulev1connect.ScheduleServiceClient
	Hooks     hookv1connect.HookServiceClient
	Worktrees worktreev1connect.WorktreeServiceClient
	Git       ReviewGit
	Keys      config.Keybindings
	Width     int
	Height    int
}

// NewPanel builds the panel the way weave panel does, over a recording tmux.
func NewPanel(params NewPanelParams) *Panel {
	tmux := &recordingTmux{
		width:       params.Width,
		height:      params.Height,
		windowWidth: params.Width,
	}
	post := newPostbox()

	return &Panel{
		model: newModel(Deps{
			PopupWindow: NewPopupWindow(tmux, "test-session"),
			Tmux:        tmux,
			Git:         params.Git,
			Agents:      params.Agents,
			Workflows:   params.Workflows,
			Schedules:   params.Schedules,
			Hooks:       params.Hooks,
			Worktrees:   params.Worktrees,
			Keys:        params.Keys,
			Layout:      config.DefaultLayout(),
			Settings:    PanelSettings{Pane: "%0"},
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
