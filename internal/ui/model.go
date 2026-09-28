// Package ui is the panel: one bubbletea program, drawn in its own tmux session, that holds no
// database connection and knows nothing the service has not told it.
//
// It is a client like any other. A keypress becomes an RPC, and the panel learns what came of
// it from the watch stream rather than by changing its own state first, so what is on screen is
// what the service last said was true. It is the largest package in the tree and README.md in
// this directory is the map of it.
package ui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"komrad/config"
	"komrad/internal/browser"
	"komrad/internal/engine"
	"komrad/internal/errui"
	"komrad/internal/git"
	"komrad/internal/glyph"
	"komrad/internal/position"
	"komrad/internal/ui/layout"
	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/hook/v1/hookv1connect"
	schedulev1connect "komrad/proto/gen/schedule/v1/schedulev1connect"
	"komrad/proto/gen/watch/v1/watchv1connect"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	"charm.land/bubbles/v2/cursor"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// sizeFallback is how long a read-back size waits for the terminal to report it.
const sizeFallback = 200 * time.Millisecond

// sizeReadMsg is the size the frame read back off tmux after moving the panel's own pane.
type sizeReadMsg struct {
	size tea.WindowSizeMsg
}

// sizeFallbackMsg is a read-back size the terminal has not confirmed within sizeFallback.
type sizeFallbackMsg struct {
	size tea.WindowSizeMsg
}

// Run launches the panel and blocks for the life of the persistent manager tmux session.
func Run(ctx context.Context, deps Deps) error {
	// The panel resizes its own pane by zooming it, and the renderer is told about that the same
	// way the terminal tells it about any other resize, so a program has to exist before the model
	// it runs can reach it.
	post := newPostbox()

	m := newModel(deps, true, post)
	program := tea.NewProgram(m, tea.WithColorProfile(colorprofile.TrueColor))
	post.attach(program)

	defer post.close()
	defer deps.Session.ReleaseCycleKeys()

	if deps.Watch != nil && m.fatal() == nil {
		watching, stop := context.WithCancel(ctx)
		defer stop()

		go watchRows(watching, deps.Watch, post)
	}

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
	// pending is the size waiting to be handed over.
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

func (p *postbox) close() {
	close(p.wake)
}

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

// send hands the program a message from off its own loop.
func (p *postbox) send(msg tea.Msg) {
	p.program.Send(msg)
}

// deliver hands the newest size to the program, off the update the size was read in, which would
// otherwise block waiting for itself to take the message.
func (p *postbox) deliver() {
	for range p.wake {
		if size := p.takePending(); size != nil {
			p.program.Send(sizeReadMsg{size: *size})
		}
	}
}

func (p *postbox) takePending() *tea.WindowSizeMsg {
	p.mu.Lock()
	defer p.mu.Unlock()

	size := p.pending
	p.pending = nil

	return size
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
	GetActivePane(pane string) string
	SendKeys(target string, keys ...string) error
}

// ReviewGit is the git the review screen reads a round through.
type ReviewGit interface {
	CountChangedFiles(ctx context.Context, params git.CountChangedFilesParams) (int, error)
	MapRanges(ctx context.Context, params git.MapRangesParams) ([]position.Mapping, error)
	Replay(ctx context.Context, params git.ReplayParams) (*git.Replayed, error)
	IsAncestor(ctx context.Context, params git.IsAncestorParams) (bool, error)
	GetRoot(ctx context.Context, dir string) (string, error)
	Diff(ctx context.Context, params git.DiffParams) ([]byte, error)
	ReadFiles(ctx context.Context, dir string, revisions []git.FileRevision) (map[git.FileRevision][]byte, error)
}

// harnessFrameReader tells a harness that has drawn itself in a pane from one that has only
// started.
type harnessFrameReader interface {
	DoesFrameShowHarness(pane, harnessID string) bool
}

// PanelSettings is what the panel was configured with, read once at launch.
type PanelSettings struct {
	// Addr is the address the service answers at.
	Addr string
	// Editor is the command a text field or a reviewed file is opened in.
	Editor string
	// Pane is the tmux pane the panel is drawn in.
	Pane string
	// DraftDir is where a copy of a document is written for the editor, the system's temporary
	// directory when empty.
	DraftDir string
}

type Deps struct {
	Session         *Session
	Tmux            uiTmux
	Git             ReviewGit
	Browser         browser.Opener
	HarnessFrames   harnessFrameReader
	Agents          agentv1connect.AgentServiceClient
	Workflows       workflowv1connect.WorkflowServiceClient
	Schedules       schedulev1connect.ScheduleServiceClient
	Hooks           hookv1connect.HookServiceClient
	Worktrees       worktreev1connect.WorktreeServiceClient
	Watch           watchv1connect.WatchServiceClient
	Keys            config.Keybindings
	Glyphs          glyph.Set
	Layout          config.Layout
	Engine          engine.Remote
	ShellCommand    []string
	TerminalCommand []string
	Binary          BinaryWatcher
	Settings        PanelSettings
	ConfigErr       error
	StartupErr      error
	StartupFindings []errui.Finding
	Engines         Engines
}

type model struct {
	session       *Session
	api           api
	tmux          uiTmux
	git           ReviewGit
	browser       browser.Opener
	harnessFrames harnessFrameReader
	binary        BinaryWatcher
	frame         *layout.Frame
	settings      PanelSettings
	unreachable   error
	engines       Engines
	engineBuild   EngineBuild
	chooser       chooserState
	switchErr     error
	// writes counts the writes sent, and a write's own count is what its landing answers to.
	writes int
	// pending is the writes in flight.
	pending         int
	configErr       error
	startupErr      error
	startupFindings []errui.Finding
	screen          screen
	height          int
	width           int
	paneCols        int

	// sized is the size each agent's own window was last put at, keyed by pane, so a listing that
	// has not moved costs no tmux calls.
	sized         map[string]string
	info          infoState
	details       detailsState
	messages      messagesState
	detailsHeight float64
	listing       listing
	reactors      []worldReactor
	agents        agentsScreen
	schedules     schedulesScreen
	archive       archiveScreen
	worktrees     worktreesScreen
	documents     documentsScreen
	// inspections is what the service answered for each worktree directory.
	inspections map[string]worktreeInspection

	search    searchScreen
	review    reviewScreen
	filter    filterState
	rowCursor cursor.Model
	form      formState
	held      heldWrite
	// detached holds the writes a person escaped out of, which land without taking the screen to
	// what they made.
	detached map[int]struct{}
	landing  landing
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
	viewsOpen     bool
	keys          config.Keybindings
	glyphs        glyph.Set
	// paneMeasure is the column one half of a split is laid out in once the split has been chosen.
	paneMeasure int
	// shell is what tmux starts a pane's process with, read once.
	shell string
	clip  clipboardWriter

	// isDark is the terminal's background as it last reported it.
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

	// docScroll is where the reader has scrolled the block beside the list to.
	docScroll docScroll

	// warm is the panes whose harness the panel has seen start, which it waits for before giving
	// one the stage.
	warm warmupState
}

// lastFrame is the frame on screen, which the panel draws again rather than drawing a new one at
// a width it is about to be told is wrong.
type lastFrame struct {
	text string
}

// measuredAt hands back a model that measures its own chrome in the column given.
func (m model) measuredAt(width int) model {
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
			worktrees: deps.Worktrees,
		},
		session:         deps.Session,
		clip:            systemClipboard{},
		tmux:            deps.Tmux,
		git:             deps.Git,
		browser:         deps.Browser,
		harnessFrames:   deps.HarnessFrames,
		binary:          deps.Binary,
		frame:           layout.New(deps.Tmux, deps.Layout, layout.WithEngine(deps.Engine), layout.WithShellCommand(deps.ShellCommand), layout.WithTerminalCommand(deps.TerminalCommand)),
		detailsHeight:   deps.Layout.DetailsHeight,
		settings:        deps.Settings,
		keys:            deps.Keys,
		glyphs:          deps.Glyphs,
		configErr:       deps.ConfigErr,
		startupErr:      deps.StartupErr,
		startupFindings: deps.StartupFindings,
		engines:         deps.Engines,
		engineBuild:     deps.Engines.Build,
		screen:          screenAgents,
		rowCursor:       newRowCursor(),
		warm:            newWarmupState(),
		reactors:        newWorldReactors(),
	}

	if m.fatal() != nil {
		return m
	}

	m.frame.OnResize(post.resize)
	m.frame.Attach(deps.Settings.Pane)
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
