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
	"github.com/charmbracelet/lipgloss"
)

// Run launches the panel and blocks for the life of the persistent manager tmux session.
// The panel is the sidebar, the resource it hovers is shown in a sibling stage pane, and
// focus moves between the two with the user's own tmux bindings.
//
// The terminal's background is settled here, before the program takes the terminal over, and
// then pinned. Asking is not a read: lipgloss writes an OSC query to the tty and blocks on the
// reply with no deadline, having turned off ECHO and ICANON on the way. Once bubbletea owns the
// input that reply is read by bubbletea instead, so the ask never returns and the panel hangs
// with whatever it last drew still on screen. Pinning the answer means no adaptive colour can
// ask again later, from any goroutine, on any screen.
func Run(deps Deps) error {
	isDark := lipgloss.HasDarkBackground()
	lipgloss.SetHasDarkBackground(isDark)

	if _, err := tea.NewProgram(newModel(deps, isDark), tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("failed to run panel: %w", err)
	}

	return nil
}

type screen int

const (
	screenAgents screen = iota
	screenForm
	screenAdopt
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
	screenReview
)

type uiTmux interface {
	layout.Tmux
	GetClientPath(client string) string
	SetInfoPane(pane string)
	SelectPane(pane string)
	OpenWindow(client, name, dir string, argv ...string) error
	SendKeys(target string, keys ...string) error
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

	Editor string

	ActionLogsDir string

	ConfigErr error

	StartupErr error

	StartupFindings []errui.Finding

	Pane string

	// Self is the weave binary, which the panel runs in the stage pane to draw into it.
	Self string
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
	review    reviewState
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

	editor        string
	actionLogsDir string

	pane string
	// self is the weave binary, which the panel runs in the stage pane to draw into it.
	self string

	clip clipboardWriter

	// isDark is the terminal's background, settled before the program started. Nothing may ask
	// lipgloss for it again while the panel is running: see Run.
	isDark bool
}

func newModel(deps Deps, isDark bool) model {
	m := model{
		isDark:          isDark,
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
		editor:          deps.Editor,
		actionLogsDir:   deps.ActionLogsDir,
		pane:            deps.Pane,
		self:            deps.Self,
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
