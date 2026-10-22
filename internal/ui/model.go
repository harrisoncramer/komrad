// Package ui is the panel: one bubbletea program, rendered in its own tmux session, that holds no
// database connection and knows nothing the server has not told it.
//
// It is a client like any other. A keypress becomes an RPC, and the panel learns what came of
// it from the watch stream rather than by changing its own state first, so what is on screen is
// what the server last said was true. It is the largest package in the tree and README.md in
// this directory is the map of it.
package ui

import (
	"cmp"
	"context"
	"fmt"
	"sync"
	"time"

	"komrad/config"
	"komrad/internal/browser"
	"komrad/internal/client"
	"komrad/internal/diff"
	"komrad/internal/engine"
	"komrad/internal/errui"
	"komrad/internal/glyph"
	"komrad/internal/loader"
	"komrad/internal/panes"
	"komrad/internal/ui/layout"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/submitting"
	"komrad/internal/vim"
	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/forge/v1/forgev1connect"
	"komrad/proto/gen/hook/v1/hookv1connect"
	"komrad/proto/gen/log/v1/logv1connect"
	"komrad/proto/gen/loop/v1/loopv1connect"
	"komrad/proto/gen/pullrequest/v1/pullrequestv1connect"
	schedulev1connect "komrad/proto/gen/schedule/v1/schedulev1connect"
	"komrad/proto/gen/task/v1/taskv1connect"
	"komrad/proto/gen/watch/v1/watchv1connect"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

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

	watching, stop := context.WithCancel(ctx)
	defer stop()

	m := newModel(deps, true, post)
	m.access.watch = func() context.CancelFunc { return startWatch(watching, deps.Watch, post) }
	wheel := newWheelFilter(time.Now, func(msg tea.Msg) {
		time.AfterFunc(wheelFrame, func() { post.send(msg) })
	})
	program := tea.NewProgram(&m, tea.WithColorProfile(colorprofile.TrueColor), tea.WithFilter(wheel.filter))
	post.attach(program)

	defer post.close()
	defer deps.Session.ReleaseLeader()

	if m.fatal() == nil && !m.isGated() {
		m.startWatch()
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
	// pending is the size waiting for handover.
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
// rendered at a width the renderer has not been told about.
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
	if p == nil || p.program == nil {
		return
	}

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
	GetShownPanes(pane string) []panes.Rect
	CaptureScreen(pane string) string
}

// harnessFrameReader tells a harness that has rendered itself in a pane from one that has only
// started.
type harnessFrameReader interface {
	DoesFrameShowHarness(pane, harnessID string) bool
}

// PanelSettings holds the panel's configuration, which the panel reads once at launch.
type PanelSettings struct {
	// Addr is the address the server answers at.
	Addr string
	// Editor is the command that opens a text field or a reviewed file.
	Editor string
	// SSHHost is the ssh host that reaches the engine's files, empty when the engine is this machine.
	SSHHost string
	// SSHConfig is the ssh config file that describes SSHHost.
	SSHConfig string
	// IsSSHIncluded is whether ~/.ssh/config reads SSHConfig, which editors that open their own window
	// need to find SSHHost.
	IsSSHIncluded bool
	// Pane is the tmux pane the panel renders in.
	Pane string
	// DraftDir is where a copy of a document is written for the editor, the system's temporary
	// directory when empty.
	DraftDir string
	// VimMode edits the text fields the way vim does.
	VimMode bool
	// DiffWrap wraps diff lines wider than the pane when the panel starts.
	DiffWrap bool
}

type Deps struct {
	Session         *Session
	Tmux            uiTmux
	Patches         diff.Reader
	Browser         browser.Opener
	HarnessFrames   harnessFrameReader
	Agents          agentv1connect.AgentServiceClient
	Workflows       workflowv1connect.WorkflowServiceClient
	Schedules       schedulev1connect.ScheduleServiceClient
	Hooks           hookv1connect.HookServiceClient
	Worktrees       worktreev1connect.WorktreeServiceClient
	Logs            logv1connect.LogServiceClient
	Forges          forgev1connect.ForgeServiceClient
	PullRequests    pullrequestv1connect.PullRequestServiceClient
	Loops           loopv1connect.LoopServiceClient
	Tasks           taskv1connect.TaskServiceClient
	Watch           watchv1connect.WatchServiceClient
	Keys            config.Keybindings
	Glyphs          glyph.Set
	Loader          loader.Style
	Layout          config.Layout
	Engine          engine.Remote
	ShellCommand    []string
	TerminalCommand []string
	Binary          BinaryWatcher
	Settings        PanelSettings
	ConfigErr       error
	StartupErr      error
	StartupFindings []errui.Finding
	EngineStatus    EngineStatus
	SignIn          SignIn
}

type model struct {
	access          accessState
	session         *Session
	api             api
	tmux            uiTmux
	patches         diff.Reader
	browser         browser.Opener
	harnessFrames   harnessFrameReader
	binary          BinaryWatcher
	frame           *layout.Frame
	settings        PanelSettings
	diffWrap        bool
	unreachable     error
	engineStatus    EngineStatus
	engineBuild     EngineBuild
	writes          writeTracker
	term            terminalState
	configErr       error
	startupErr      error
	startupFindings []errui.Finding
	screen          screen
	terminalDir     string
	details         detailsState
	messages        messagesState
	detailsHeight   float64
	listing         panel.Listing
	reactors        []worldReactor
	agents          agentsScreen
	schedules       schedulesScreen
	worktrees       worktreesScreen
	documents       documentsScreen
	logs            logsScreen
	statusChecks    statusChecksScreen
	statusCheckLogs statusCheckLogsScreen
	// inspections is what the server answered for each worktree directory.
	inspections map[string]worktreeInspection
	lastScreen  lastScreen
	said        saidTranscript
	review      reviewScreen
	filter      filterState
	form        formState
	// register is the text vim last yanked or deleted in any field, nil when vim is off.
	register *vim.Register
	// edited is the files a reader opened an editor on over an agent's worktree while it was
	// paused, keyed by agent, so whoever lets that agent go can tell it where to look.
	edited map[string][]string
	// unstaging is the panes taken off the stage for a write still in flight that kills them.
	unstaging    map[string]struct{}
	helpExpanded bool
	topHeld      bool
	viewsOpen    bool
	keys         config.Keybindings
	glyphs       glyph.Set
	// shell is what tmux starts a pane's process with, read once.
	shell string
	clip  clipboardWriter
	post  *postbox

	// staging counts the cursor moves waiting for the panes to move, so the panes follow the row the
	// cursor came to rest on rather than every row it passed.
	staging int

	// reading is where the reader stands in the document beside the list.
	reading docReader

	// warm is the panes whose harness the panel has seen start, which it waits for before giving
	// one the stage.
	warm    warmupState
	loading loadingState

	// picker is the window as the pane picker froze it.
	picker      pickerState
	wheelHit    wheelHit
	detailsFits derived[shownScreen, bool]

	// leading is whether the leader was pressed and the key after it is still to come.
	leading bool

	// leftStop is the stop the keys last left.
	leftStop stop
}

type writeTracker struct {
	// count is the writes sent, and a write's own count is what its landing answers to.
	count int
	// inflight is the writes sent that have not landed, by their count.
	inflight map[int]submitting.Write
	held     heldWrite
	// detached holds the writes a person escaped out of, which land without taking the screen to
	// what they made.
	detached map[int]struct{}
	// parked is the answered writes held until the panel shows what they did, by their count.
	parked   map[int]parkedWrite
	deferred client.WatchBatch
}

type terminalState struct {
	height   int
	width    int
	paneCols int
	// sized is the size each agent's own window was last put at, keyed by pane, so a listing that
	// has not moved costs no tmux calls.
	sized map[string]string
	// blurred is whether another pane holds the keyboard.
	blurred bool
	// keysElsewhere is whether the keyboard sits on a pane beside the panel, which the terminal
	// itself losing focus is not.
	keysElsewhere bool
	// paneMeasure is the column that holds one half of a split once the split is chosen.
	paneMeasure int
	// isDark is the terminal's background as it last reported it.
	isDark bool
	// awaitingSince is when the panel moved its own pane without yet learning the size that
	// leaves it, and zero while it is not waiting.
	awaitingSince time.Time
	// windowCols is how wide the window was when the divider was last read.
	windowCols int
	// renderedView is the frame View last rendered, emptied by every update unless the panel awaits its size.
	renderedView string
	// copying is whether a stand-in in copy-mode has the sidebar's place.
	copying bool
	// paneCopying is whether the pane holding the keys, other than the sidebar, is in copy-mode.
	paneCopying bool
	// markingZones is whether this frame carries the marks that say where each block was rendered.
	markingZones bool
}

// measuredAt hands back a model that measures its own chrome in the column given.
func (m *model) measuredAt(width int) *model {
	measured := *m
	measured.term.paneMeasure = width

	return &measured
}

func newModel(deps Deps, isDark bool, post *postbox) model {
	m := model{
		term: terminalState{
			isDark: isDark,
		},
		post: post,
		api: api{
			agents:       deps.Agents,
			workflows:    deps.Workflows,
			schedules:    deps.Schedules,
			hooks:        deps.Hooks,
			worktrees:    deps.Worktrees,
			logs:         deps.Logs,
			forges:       deps.Forges,
			pullRequests: deps.PullRequests,
			loops:        deps.Loops,
			tasks:        deps.Tasks,
		},
		session:         deps.Session,
		clip:            systemClipboard{},
		tmux:            deps.Tmux,
		patches:         deps.Patches,
		browser:         deps.Browser,
		harnessFrames:   deps.HarnessFrames,
		binary:          deps.Binary,
		frame:           layout.New(deps.Tmux, deps.Layout, layout.WithEngine(deps.Engine), layout.WithShellCommand(deps.ShellCommand), layout.WithTerminalCommand(deps.TerminalCommand)),
		detailsHeight:   deps.Layout.DetailsHeight,
		settings:        deps.Settings,
		register:        newRegister(deps.Settings.VimMode),
		diffWrap:        deps.Settings.DiffWrap,
		keys:            deps.Keys,
		glyphs:          deps.Glyphs,
		configErr:       deps.ConfigErr,
		startupErr:      deps.StartupErr,
		startupFindings: deps.StartupFindings,
		engineStatus:    deps.EngineStatus,
		engineBuild:     deps.EngineStatus.Build,
		screen:          screenAgents,
		agents: agentsScreen{
			triage:   triageZone{capacity: deps.Layout.TriageRows},
			archived: archivedAgents{kind: agentsArchived},
			deleted:  archivedAgents{kind: agentsDeleted},
		},
		worktrees: worktreesScreen{sort: tabsState{flat: true}},
		warm:      newWarmupState(),
		loading:   loadingState{style: cmp.Or(deps.Loader, loader.Braille)},
		reactors:  newWorldReactors(),
	}

	if m.fatal() != nil {
		return m
	}

	if deps.SignIn != nil {
		m.access = accessState{signIn: deps.SignIn, deps: deps, stage: accessChecking}

		return m
	}

	m.attachFrame()

	return m
}

func (m *model) attachFrame() {
	m.frame.OnResize(m.post.resize)
	m.frame.Attach(m.settings.Pane)
	m.shell = m.tmux.GetDefaultShell()
}
