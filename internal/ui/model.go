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

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Run launches the panel and blocks for the life of the persistent manager tmux session.
func Run(deps Deps) error {
	// Asking writes an OSC query to the tty and blocks on the reply with no deadline, so the
	// answer is settled and pinned here before bubbletea owns the input and reads that reply.
	isDark := lipgloss.HasDarkBackground()
	lipgloss.SetHasDarkBackground(isDark)

	if _, err := tea.NewProgram(newModel(deps, isDark), tea.WithAltScreen(), tea.WithReportFocus()).Run(); err != nil {
		return fmt.Errorf("failed to run panel: %w", err)
	}

	return nil
}

type uiTmux interface {
	layout.Tmux
	ResizeWindow(pane string, width, height int)
	SetSessionOption(target, name, value string)
	GetHostPath() string
	GetDefaultShell() string
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
	Layout          config.Layout
	Addr            string
	LogViewer       string
	LogPath         string
	ProcessViewer   string
	Editor          string
	RunLogsDir      string
	RoundsDir       string
	ConfigErr       error
	StartupErr      error
	StartupFindings []errui.Finding
	Pane            string
	FocusKey        string
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

	// sized is the size each agent's own window was last put at, keyed by pane, so a listing that
	// has not moved costs no tmux calls.
	sized map[string]string

	info          infoState
	details       detailsState
	detailsHeight float64
	listing       listing
	tree          treeState
	hover         hoverState
	runHover      hoverState
	schedules     scheduleState
	archive       archiveState
	search        searchState
	review        reviewState
	mail          mailState
	filter        filterState
	rowCursor     cursor.Model
	column        columnState
	marks         markState
	form          formState
	held          heldWrite
	// detached holds the writes a person escaped out of, which land without taking the screen
	// to what they made since whoever walked away is somewhere else by then.
	detached map[int]struct{}
	// blurred is whether another pane holds the keyboard, which stops the row cursor blinking
	// since a panel nobody is typing at has nothing to blink for.
	blurred       bool
	helpExpanded  bool
	topHeld       bool
	keys          config.Keybindings
	logPath       string
	logViewer     string
	processViewer string
	editor        string
	runLogsDir    string
	roundsDir     string
	pane          string
	focusKey      string
	// paneMeasure is the column a screen is measuring its own chrome in while it works out whether
	// its split leaves room for it, which paneWidth reads because it is the answer that
	// measurement feeds.
	paneMeasure int
	// shell is what tmux starts a pane's process with, read once since the terminal has to name
	// it rather than leave the pane to rerun whatever was in it last.
	shell string
	clip  clipboardWriter

	// isDark is the terminal's background, settled before the program started and never asked of
	// lipgloss again while the panel runs.
	isDark bool
	// resizeOwed says the zoom moved inside a change that was still being collected, so the
	// panel's own size is read back once the change has landed rather than before it.
	resizeOwed bool
}

// at is the only writer of paneMeasure, handing back a model that measures its own chrome in a
// column named outright rather than asking which column it has, which is the question a screen
// deciding its own split is still in the middle of.
func (m model) at(width int) model {
	m.paneMeasure = width

	return m
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
		roundsDir:       deps.RoundsDir,
		pane:            deps.Pane,
		focusKey:        deps.FocusKey,
		keys:            deps.Keys,
		configErr:       deps.ConfigErr,
		startupErr:      deps.StartupErr,
		startupFindings: deps.StartupFindings,
		screen:          screenAgents,
		rowCursor:       newRowCursor(),
	}

	if m.fatal() != nil {
		return m
	}

	m.frame.Attach(deps.Pane)
	m.shell = deps.Tmux.GetDefaultShell()

	return m
}

func (m model) currentDir(dir string) string {
	if trimmed := strings.TrimSpace(dir); trimmed != "" {
		return trimmed
	}
	if path := m.tmux.GetHostPath(); path != "" {
		return path
	}

	home, _ := os.UserHomeDir()

	return home
}

func clampCursor(idx, length int) int {
	return max(min(idx, length-1), 0)
}
