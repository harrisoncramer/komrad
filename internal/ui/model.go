// Package ui is the panel: one bubbletea program, drawn inside a tmux popup, that holds no
// database connection and knows nothing the service has not told it.
//
// It is a client like any other. A keypress becomes an RPC, and the panel learns what came of
// it on its next refresh rather than by changing its own state first, so what is on screen is
// what the service last said was true. It is the largest package in the tree and README.md in
// this directory is the map of it.
package ui

import (
	"fmt"
	"os"
	"strings"

	"weave/config"
	"weave/internal/errui"
	"weave/internal/ui/layout"
	"weave/proto/gen/agent/v1/agentv1connect"
	"weave/proto/gen/hook/v1/hookv1connect"
	schedulev1connect "weave/proto/gen/schedule/v1/schedulev1connect"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Run launches the panel and blocks for the life of the persistent manager tmux session.
func Run(deps Deps) error {
	// Asking writes an OSC query to the tty and blocks on the reply with no deadline, so the
	// answer is settled and pinned here before bubbletea owns the input and reads that reply.
	isDark := lipgloss.HasDarkBackground()
	lipgloss.SetHasDarkBackground(isDark)

	if _, err := tea.NewProgram(newModel(deps, isDark), tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("failed to run panel: %w", err)
	}

	return nil
}

type uiTmux interface {
	layout.Tmux
	GetClientPath(client string) string
	DefaultShell() string
	SetInfoPane(pane string)
	SelectPane(pane string)
	SendKeys(target string, keys ...string) error
}

type Deps struct {
	PopupWindow     *PopupWindow
	Tmux            uiTmux
	Agents          agentv1connect.AgentServiceClient
	Workflows       workflowv1connect.WorkflowServiceClient
	Schedules       schedulev1connect.ScheduleServiceClient
	Hooks           hookv1connect.HookServiceClient
	Keys            config.Keybindings
	Tags            []config.Tag
	Layout          config.Layout
	Addr            string
	LogViewer       string
	LogPath         string
	ProcessViewer   string
	Editor          string
	RunLogsDir      string
	ConfigErr       error
	StartupErr      error
	StartupFindings []errui.Finding
	Pane            string

	// Self is the weave binary, which the panel runs in the stage pane to draw into it.
	Self string
}

type model struct {
	popup           *PopupWindow
	api             api
	tmux            uiTmux
	frame           *layout.Frame
	addr            string
	unreachable     error
	epoch           int
	pending         int
	configErr       error
	startupErr      error
	startupFindings []errui.Finding
	banner          *errui.Banner
	screen          screen
	height          int
	width           int
	info            infoState
	details         detailsState
	detailsHeight   float64
	listing         listing
	tree            treeState
	hover           hoverState
	runHover        hoverState
	schedules       scheduleState
	archive         archiveState
	search          searchState
	review          reviewState
	mail            mailState
	filter          filterState
	form            formState
	pendingForm     bool
	setupPending    bool
	helpExpanded    bool
	topHeld         bool
	keys            config.Keybindings
	tags            []config.Tag
	logPath         string
	logViewer       string
	processViewer   string
	editor          string
	runLogsDir      string
	pane            string
	// self is the weave binary, which the panel runs in the stage pane to draw into it.
	self string
	// shell is what tmux starts a pane's process with, read once since the terminal has to name
	// it rather than leave the pane to rerun whatever was in it last.
	shell string
	clip  clipboardWriter

	// isDark is the terminal's background, settled before the program started and never asked of
	// lipgloss again while the panel runs.
	isDark bool
}

func newModel(deps Deps, isDark bool) model {
	m := model{
		isDark: isDark,
		api: api{
			agents:    deps.Agents,
			workflows: deps.Workflows,
			schedules: deps.Schedules,
			hooks:     deps.Hooks,
		},
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
		runLogsDir:      deps.RunLogsDir,
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
	m.shell = deps.Tmux.DefaultShell()

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
