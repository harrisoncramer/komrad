package ui

import (
	"fmt"
	"os"
	"strings"

	"weave/config"
	"weave/internal/errui"
	"weave/internal/ui/layout"
	"weave/proto/gen/action/v1/actionv1connect"
	"weave/proto/gen/agent/v1/agentv1connect"
	schedulev1connect "weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// Run launches the panel and blocks for the life of the persistent manager tmux session.
// The panel is the sidebar, the resource it hovers is shown in a sibling stage pane, and
// focus moves between the two with the user's own tmux bindings.
func Run(deps Deps) error {
	if _, err := tea.NewProgram(newModel(deps), tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("failed to run panel: %w", err)
	}

	return nil
}

type screen int

const (
	screenAgents screen = iota
	screenForm
	screenRename
	screenAgentArchive
	screenWorkflowForm
	screenWorkflowArchive
	screenSearch
	screenSchedules
	screenScheduleForm
	screenScheduleArchive
	screenArchive
	screenArchiveDelete
	screenArchiveEmpty
	screenActions
)

type uiTmux interface {
	layout.Tmux
	GetClientPath(client string) string
	SetInfoPane(pane string)
	OpenWindow(client, name, dir string, argv ...string) error
}

type Deps struct {
	PopupWindow *PopupWindow
	Tmux        uiTmux
	Agents      agentv1connect.AgentServiceClient
	Workflows   workflowv1connect.WorkflowServiceClient
	Schedules   schedulev1connect.ScheduleServiceClient
	Actions     actionv1connect.ActionServiceClient
	Keys        config.Keybindings

	Tags []config.Tag

	Layout config.Layout

	Addr string

	LogViewer string

	LogPath string

	ProcessViewer string

	ActionLogsDir string

	ConfigErr error

	StartupErr error

	StartupFindings []errui.Finding

	Pane string
}

type model struct {
	popup *PopupWindow
	api   api

	tmux uiTmux

	frame *layout.Frame

	addr string

	unreachable error

	epoch int

	pending int

	configErr       error
	startupErr      error
	startupFindings []errui.Finding

	banner *errui.Banner

	screen screen

	height int
	width  int

	info infoState

	details       detailsState
	detailsHeight float64

	listing listing

	tree      treeState
	schedules scheduleState
	archive   archiveState
	search    searchState
	actions   actionState

	filter filterState

	form   *huh.Form
	values formValues
	target formTarget

	pendingForm bool

	setupPending bool

	helpExpanded bool

	topHeld bool

	keys config.Keybindings
	tags []config.Tag

	logPath   string
	logViewer string

	processViewer string
	actionLogsDir string

	clip clipboardWriter
}

func newModel(deps Deps) model {
	m := model{
		api:             api{agents: deps.Agents, workflows: deps.Workflows, schedules: deps.Schedules, actions: deps.Actions},
		popup:           deps.PopupWindow,
		clip:            systemClipboard{},
		tmux:            deps.Tmux,
		frame:           layout.New(deps.Tmux, deps.Layout),
		detailsHeight:   deps.Layout.DetailsHeight,
		addr:            deps.Addr,
		logPath:         deps.LogPath,
		logViewer:       deps.LogViewer,
		processViewer:   deps.ProcessViewer,
		actionLogsDir:   deps.ActionLogsDir,
		keys:            deps.Keys,
		tags:            deps.Tags,
		configErr:       deps.ConfigErr,
		startupErr:      deps.StartupErr,
		startupFindings: deps.StartupFindings,
		screen:          screenAgents,
	}

	if m.fatal() != nil {
		return m
	}

	m.frame.Attach(deps.Pane)

	return m
}

func (m model) currentDir(dir string) string {
	if trimmed := strings.TrimSpace(dir); trimmed != "" {
		return trimmed
	}
	if path := m.tmux.GetClientPath(m.popup.InvokingClient()); path != "" {
		return path
	}

	home, _ := os.UserHomeDir()

	return home
}

func clampCursor(idx, length int) int {
	return max(min(idx, length-1), 0)
}
