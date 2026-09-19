// Package ui is the panel: one bubbletea program, drawn inside a tmux popup, that holds no
// database connection and knows nothing the service has not told it.
//
// It is a client like any other. A keypress becomes an RPC, and the panel learns what came of
// it on its next refresh rather than by changing its own state first, so what is on screen is
// what the service last said was true. It is the largest package in the tree and README.md in
// this directory is the map of it.
package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"weave/config"
	"weave/internal/engine"
	"weave/internal/errui"
	"weave/internal/git"
	"weave/internal/position"
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

	// The panel resizes its own pane by zooming it, and the renderer is told about that the same
	// way the terminal tells it about any other resize, so a program has to exist before the model
	// it runs can reach it.
	post := newPostbox()

	program := tea.NewProgram(newModel(deps, isDark, post), tea.WithAltScreen(), tea.WithReportFocus())
	post.attach(program)

	defer post.close()
	defer deps.PopupWindow.ReleaseFocusKey()

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("failed to run panel: %w", err)
	}

	return nil
}

// postbox is how the panel sends itself the size its own pane landed at, held as a pointer so the
// model can be built before the program that carries it.
type postbox struct {
	program *tea.Program
	mu      sync.Mutex
	// pending is the size waiting to be handed over, which is only ever the newest one.
	pending *tea.WindowSizeMsg
	wake    chan struct{}
}

func newPostbox() *postbox {
	return &postbox{wake: make(chan struct{}, 1)}
}

// attach gives the postbox the program it posts to and starts the one goroutine that does it, so
// the sizes arrive in the order they were read.
func (p *postbox) attach(program *tea.Program) {
	p.program = program

	go p.deliver()
}

func (p *postbox) close() { close(p.wake) }

// resize reports the size the panel's own pane landed at as the resize it is, so a frame is never
// drawn at a width the renderer has not been told about.
func (p *postbox) resize(width, height int) {
	if p == nil {
		return
	}

	p.mu.Lock()
	p.pending = &tea.WindowSizeMsg{Width: width, Height: height}
	p.mu.Unlock()

	// The delivery is already owed when the wake is full, and it will read whatever is pending by
	// the time it runs.
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

// deliver hands the newest size to the program, off the update the size was read in, which would
// otherwise block waiting for itself to take the message.
func (p *postbox) deliver() {
	for range p.wake {
		p.mu.Lock()
		size := p.pending
		p.pending = nil
		p.mu.Unlock()

		if size != nil {
			p.program.Send(*size)
		}
	}
}

type uiTmux interface {
	layout.Tmux
	ResizeWindow(pane string, width, height int)
	IsPaneRunningCLI(pane string) bool
	SetSessionOption(target, name, value string)
	GetHostPath() string
	GetDefaultShell() string
	SetInfoPane(pane string)
	SelectPane(pane string)
	SendKeys(target string, keys ...string) error
}

// uiGit is the git the review screen reads a round through.
type uiGit interface {
	ReadFileLines(ctx context.Context, dir, sha, path string) ([]string, error)
	CountChangedFiles(ctx context.Context, params git.CountChangedFilesParams) (int, error)
	MapRanges(ctx context.Context, params git.MapRangesParams) ([]position.Mapping, error)
	Replay(ctx context.Context, params git.ReplayParams) (*git.Replayed, error)
	IsAncestor(ctx context.Context, dir, maybeAncestor, of string) (bool, error)
	ProbeTree(ctx context.Context, dir string) (string, error)
	GetTree(ctx context.Context, dir, sha string) (string, error)
	GetRoot(ctx context.Context, dir string) (string, error)
	Diff(ctx context.Context, params git.DiffParams) ([]byte, error)
	ReadFile(ctx context.Context, dir, sha, path string) ([]byte, error)
}

// harnessFrameReader tells a harness that has drawn itself in a pane from one that has only
// started.
type harnessFrameReader interface {
	DoesFrameShowHarness(pane, harnessID string) bool
}

type Deps struct {
	PopupWindow     *PopupWindow
	Tmux            uiTmux
	Git             uiGit
	HarnessFrames   harnessFrameReader
	Agents          agentv1connect.AgentServiceClient
	Workflows       workflowv1connect.WorkflowServiceClient
	Schedules       schedulev1connect.ScheduleServiceClient
	Hooks           hookv1connect.HookServiceClient
	Keys            config.Keybindings
	Layout          config.Layout
	Engine          engine.Remote
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
}

type model struct {
	popup           *PopupWindow
	api             api
	tmux            uiTmux
	git             uiGit
	harnessFrames   harnessFrameReader
	frame           *layout.Frame
	addr            string
	unreachable     error
	epoch           int
	pending         int
	configErr       error
	startupErr      error
	startupFindings []errui.Finding
	screen          screen
	height          int
	width           int

	// sized is the size each agent's own window was last put at, keyed by pane, so a listing that
	// has not moved costs no tmux calls.
	sized         map[string]string
	info          infoState
	details       detailsState
	messages      messagesState
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
	// detached holds the writes a person escaped out of, which land without taking the screen to
	// what they made.
	detached map[int]struct{}
	// edited is the files a reader opened an editor on over an agent's worktree while it was
	// paused, keyed by agent, so whoever lets that agent go can tell it where to look.
	edited map[string][]string
	// blurred is whether another pane holds the keyboard.
	blurred bool

	// keysElsewhere is whether the keyboard sits on a pane beside the panel, which the terminal
	// itself losing focus is not.
	keysElsewhere bool
	helpExpanded  bool
	topHeld       bool
	keys          config.Keybindings
	logPath       string
	logViewer     string
	processViewer string
	editor        string
	runLogsDir    string
	pane          string
	// paneMeasure is the column one half of a split is laid out in once the split has been chosen.
	paneMeasure int
	// shell is what tmux starts a pane's process with, read once.
	shell string
	clip  clipboardWriter

	// isDark is the terminal's background, settled before the program started and never asked
	// again.
	isDark bool
	post   *postbox

	// staging counts the cursor moves waiting to be staged, so the panes are moved for the row the
	// cursor came to rest on rather than for every row it passed.
	staging int

	// awaitingSize says the panel has moved its own pane and has not been told the size that
	// leaves it.
	awaitingSize bool

	// windowCols is how wide the window was when the divider was last read.
	windowCols int

	// drawn is the last frame the panel put on screen, held through a pointer so a view can keep
	// what it drew.
	drawn *lastFrame

	// warm is the panes whose harness the panel has seen start, which it waits for before giving
	// one the stage.
	warm warmupState
}

// lastFrame is the frame on screen, which the panel draws again rather than drawing a new one at
// a width it is about to be told is wrong.
type lastFrame struct {
	text string
}

// at is the only writer of paneMeasure, handing back a model that measures its own chrome in a
// column named outright rather than asking which column it has.
func (m model) at(width int) model {
	m.paneMeasure = width

	return m
}

func newModel(deps Deps, isDark bool, post *postbox) model {
	m := model{
		isDark: isDark,
		post:   post,
		drawn:  &lastFrame{},
		api: api{
			agents:    deps.Agents,
			workflows: deps.Workflows,
			schedules: deps.Schedules,
			hooks:     deps.Hooks,
		},
		popup:           deps.PopupWindow,
		clip:            systemClipboard{},
		tmux:            deps.Tmux,
		git:             deps.Git,
		harnessFrames:   deps.HarnessFrames,
		frame:           layout.New(deps.Tmux, deps.Layout, layout.WithEngine(deps.Engine)),
		detailsHeight:   deps.Layout.DetailsHeight,
		addr:            deps.Addr,
		logPath:         deps.LogPath,
		logViewer:       deps.LogViewer,
		processViewer:   deps.ProcessViewer,
		editor:          deps.Editor,
		runLogsDir:      deps.RunLogsDir,
		pane:            deps.Pane,
		keys:            deps.Keys,
		configErr:       deps.ConfigErr,
		startupErr:      deps.StartupErr,
		startupFindings: deps.StartupFindings,
		screen:          screenAgents,
		rowCursor:       newRowCursor(),
		warm:            newWarmupState(),
	}

	if m.fatal() != nil {
		return m
	}

	m.frame.OnResize(post.resize)
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
