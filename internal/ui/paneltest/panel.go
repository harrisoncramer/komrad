// Package paneltest drives the panel the way a person, the server and the terminal do, and reads
// back what they would see.
package paneltest

import (
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/browser"
	"komrad/internal/client"
	"komrad/internal/diff"
	"komrad/internal/engine"
	"komrad/internal/gittest"
	"komrad/internal/loader"
	"komrad/internal/ui"
	"komrad/internal/ui/keys"
	watchv1 "komrad/proto/gen/watch/v1"

	tea "charm.land/bubbletea/v2"
	"connectrpc.com/connect"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
)

// The size a panel under test is given when its story names none.
const (
	DefaultWidth  = 140
	DefaultHeight = 40
)

// Rest is a press that lets the cursor come to rest on the row it was moved to.
const Rest = "<rest>"

// restWait is how long a cursor rests before the panel stages what it stands on.
const restWait = 90 * time.Millisecond

// Params is what a panel under test is built over.
type Params struct {
	Width         int
	Height        int
	DetailsHeight float64
	SidebarWidth  int
	TriageRows    int
	Keys          *config.Keybindings
	Git           *gittest.Repo
	Settings      ui.PanelSettings
	Engine        engine.Remote
	ShellCommand  []string
	// TerminalCommand is what the stage attaches to a remote agent with.
	TerminalCommand []string
	HarnessFrames   ui.HarnessFrameReader
	ConfigErr       error
	StartupErr      error
	RefuseSplits    bool
	Binary          ui.BinaryWatcher
	Browser         browser.Opener
	// Tmux is a real tmux the recorded one passes every call on to.
	Tmux ui.Tmux
	// Remote is the server clients to send requests through in place of the recording server.
	Remote       *Remote
	EngineStatus ui.EngineStatus
	Loader       loader.Style
	SignIn       ui.SignIn
}

// Panel is a panel under test and everything it talks to.
type Panel struct {
	t        *testing.T
	panel    *ui.Panel
	pane     string
	tmux     *recordedTmux
	recorder *recorder
	gate     *gate
	clock    *clock
	inbox    *inbox
	watcher  *watcher
	editor   *editor
	clip     *clipboard
	waits    []wait
	armed    int
	parked   []parkedCmd
	// sent is the size the terminal last told the panel its pane is.
	sent tea.WindowSizeMsg
	quit bool
}

type watcher struct {
	mu       sync.Mutex
	onBatch  func(client.WatchBatch)
	onLost   func(error)
	watching bool
}

type editor struct {
	mu     sync.Mutex
	next   *string
	opened []string
}

type clipboard struct {
	mu      sync.Mutex
	written []string
	err     error
}

// Start builds the panel the way komrad panel does, over a recorded tmux and a recording server.
func Start(t *testing.T, params Params) *Panel {
	t.Helper()

	gate := &gate{
		held:   map[string]bool{},
		parked: make(chan parkedCall),
	}
	recorder := &recorder{
		answers: map[string]AnswerFunc{},
		gate:    gate,
	}

	remote := params.Remote
	if remote == nil {
		remote = newRemote(t, recorder)
	}

	keybindings := config.DefaultKeybindings()
	if params.Keys != nil {
		keybindings = *params.Keys
	}

	var patches diff.Reader
	if params.Git != nil {
		require.Nil(t, params.Git.Before, "a repo is held by one panel")
		params.Git.Before = gate.wait
		patches = params.Git
	}

	width, height := params.Width, params.Height
	if width == 0 {
		width = DefaultWidth
	}
	if height == 0 {
		height = DefaultHeight
	}

	settings := params.Settings
	if settings.Pane == "" {
		settings.Pane = sidebarPane
	}
	if settings.DraftDir == "" {
		settings.DraftDir = t.TempDir()
	}

	layout := config.DefaultLayout()
	if params.DetailsHeight != 0 {
		layout.DetailsHeight = params.DetailsHeight
	}
	if params.SidebarWidth != 0 {
		layout.SidebarWidth = params.SidebarWidth
	}
	layout.TriageRows = params.TriageRows

	tmux := newTmux(params.Tmux, settings.Pane, width, height)
	tmux.refuseSplits = params.RefuseSplits

	p := &Panel{
		t:        t,
		pane:     settings.Pane,
		tmux:     tmux,
		recorder: recorder,
		gate:     gate,
		clock:    &clock{now: time.Now()},
		inbox:    &inbox{},
		watcher:  &watcher{},
		editor:   &editor{},
		clip:     &clipboard{},
	}
	p.panel = ui.New(ui.Deps{
		Session: ui.NewSession(ui.NewSessionParams{
			Tmux:        tmux,
			Session:     "test-session",
			LeaderKey:   "C-Space",
			LeaderTable: "komrad-leader",
		}),
		Tmux:            tmux,
		Patches:         patches,
		Browser:         params.Browser,
		HarnessFrames:   params.HarnessFrames,
		Agents:          remote.Agents,
		Workflows:       remote.Workflows,
		Schedules:       remote.Schedules,
		Hooks:           remote.Hooks,
		Worktrees:       remote.Worktrees,
		Logs:            remote.Logs,
		Forges:          remote.Forges,
		PullRequests:    remote.PullRequests,
		Loops:           remote.Loops,
		Tasks:           remote.Tasks,
		Watcher:         p.watcher,
		Clock:           p.clock,
		Clipboard:       p.clip,
		Editor:          p.editor,
		Keys:            keybindings,
		Loader:          params.Loader,
		Layout:          layout,
		Engine:          params.Engine,
		ShellCommand:    params.ShellCommand,
		TerminalCommand: params.TerminalCommand,
		Binary:          params.Binary,
		Settings:        settings,
		ConfigErr:       params.ConfigErr,
		StartupErr:      params.StartupErr,
		EngineStatus:    params.EngineStatus,
		SignIn:          params.SignIn,
	})
	tmux.rendered = func() string { return p.panel.View().Content }
	p.panel.Attach(p.inbox)
	t.Cleanup(p.releaseParked)

	p.settle(p.panel.Init())
	p.Send(tea.WindowSizeMsg{Width: width, Height: height})

	return p
}

// Press types each key into the panel, Rest letting the cursor come to rest and pick:<to> landing
// the keys on a sidebar list, record or block, or on a pane, through the pane picker.
func (p *Panel) Press(presses ...string) {
	p.t.Helper()

	for _, press := range presses {
		if press == Rest {
			p.Advance(restWait)

			continue
		}

		if to, ok := strings.CutPrefix(press, "pick:"); ok {
			p.pick(to)

			continue
		}

		p.Send(keys.Press(press))
	}
}

// Send hands the panel a message from its terminal, such as a size, a click or a turn of the wheel.
func (p *Panel) Send(msg tea.Msg) {
	p.t.Helper()

	p.settle(p.update(msg))
	p.answerTerminal()
}

// Watch hands the panel a batch off the last watch stream it opened, which may since have stopped.
func (p *Panel) Watch(events ...*watchv1.WatchEvent) {
	p.t.Helper()

	p.watcher.mu.Lock()
	onBatch := p.watcher.onBatch
	p.watcher.mu.Unlock()
	if onBatch == nil {
		p.t.Fatalf("the panel never opened its watch stream")
	}

	onBatch(client.WatchBatch(events))
	p.settle(nil)
	p.answerTerminal()
}

// LoseWatch ends the last watch stream the panel opened with err.
func (p *Panel) LoseWatch(err error) {
	p.t.Helper()

	p.watcher.mu.Lock()
	onLost := p.watcher.onLost
	p.watcher.mu.Unlock()
	if onLost == nil {
		p.t.Fatalf("the panel never opened its watch stream")
	}

	onLost(err)
	p.settle(nil)
	p.answerTerminal()
}

// Advance moves the clock on, firing every wait that falls due in the order they fall due.
func (p *Panel) Advance(by time.Duration) {
	p.t.Helper()

	target := p.clock.Now().Add(by)
	for {
		due, ok := p.takeDue(target)
		if !ok {
			break
		}

		if due.due.After(p.clock.Now()) {
			p.clock.set(due.due)
		}
		msg := due.fire(due.due)
		p.settle(func() tea.Msg { return msg })
		p.answerTerminal()
	}

	p.clock.set(target)
}

// WriteInEditor is the text the editor writes to its file when the panel next opens it.
func (p *Panel) WriteInEditor(text string) {
	p.editor.mu.Lock()
	defer p.editor.mu.Unlock()

	p.editor.next = &text
}

// Answer answers every later call to a procedure with answer.
func (p *Panel) Answer(procedure string, answer AnswerFunc) {
	p.recorder.answer(procedure, answer)
}

// Refuse fails every later call to a procedure with err.
func (p *Panel) Refuse(procedure string, err error) {
	p.recorder.answer(procedure, func(connect.AnyRequest) (connect.AnyResponse, error) {
		return nil, err
	})
}

// Hold keeps every later call to a procedure or a git read from answering until it is released.
func (p *Panel) Hold(name string) {
	p.gate.mu.Lock()
	defer p.gate.mu.Unlock()

	p.gate.held[name] = true
}

// Release lets the first held call to a procedure or a git read answer.
func (p *Panel) Release(name string) {
	p.t.Helper()

	for i, parked := range p.parked {
		if parked.call.name != name {
			continue
		}

		p.parked = append(p.parked[:i:i], p.parked[i+1:]...)
		close(parked.call.release)
		msg, isDone := p.await(parked.done)
		if isDone {
			p.settle(func() tea.Msg {
				return msg
			})
			p.answerTerminal()
		}

		return
	}

	require.Failf(p.t, "nothing to release", "no call to %s is held", name)
}

// Held is how many calls to a procedure wait on Release.
func (p *Panel) Held(name string) int {
	count := 0
	for _, parked := range p.parked {
		if parked.call.name == name {
			count++
		}
	}

	return count
}

// Quits reports whether pressing key quits the panel.
func (p *Panel) Quits(key string) bool {
	p.t.Helper()

	p.quit = false
	p.Press(key)

	return p.quit
}

// Now is the panel's clock.
func (p *Panel) Now() time.Time {
	return p.clock.Now()
}

// Frame is the frame the panel renders inside its gutter and edge, without its styles.
func (p *Panel) Frame() string {
	return ansi.Strip(p.RawFrame())
}

// TypedFrame is the frame with the character a text cursor stands on written as [c].
func (p *Panel) TypedFrame() string {
	return ansi.Strip(typedCursor.ReplaceAllString(p.RawFrame(), "[$1]"))
}

// Rendered is the whole frame the panel renders, gutter and edge included, without its styles.
func (p *Panel) Rendered() string {
	return ansi.Strip(p.panel.View().Content)
}

// RawFrame is the frame the panel renders inside its gutter and edge, styles included.
func (p *Panel) RawFrame() string {
	lines := strings.Split(p.panel.View().Content, "\n")
	right := p.sent.Width
	if hasEdge(lines, right) {
		right -= edgeCols
	}

	for i, line := range lines {
		lines[i] = strings.TrimRight(emptyStyle.ReplaceAllString(ansi.Cut(line, gutterCols, right), ""), " ")
	}

	return strings.Join(lines, "\n")
}

// Requests is every request the panel has sent, in the order it sent them.
func (p *Panel) Requests() []Request {
	return p.recorder.requests()
}

// AssertSent asserts the panel has sent exactly these requests.
func (p *Panel) AssertSent(want ...Request) {
	p.t.Helper()

	p.AssertSentWith(nil, want...)
}

// AssertSentWith asserts the panel has sent exactly these requests, compared with opts.
func (p *Panel) AssertSentWith(opts []cmp.Option, want ...Request) {
	p.t.Helper()

	diff := cmp.Diff(want, p.Requests(), append([]cmp.Option{protocmp.Transform(), cmpopts.EquateEmpty()}, opts...)...)
	assert.Empty(p.t, diff, "the requests sent (-want +got)")
}

// AssertSentTo asserts the panel has sent exactly these requests to a procedure.
func (p *Panel) AssertSentTo(procedure string, want ...proto.Message) {
	p.t.Helper()

	p.AssertSentToWith(procedure, nil, want...)
}

// AssertSentToWith asserts the panel has sent exactly these requests to a procedure, compared with opts.
func (p *Panel) AssertSentToWith(procedure string, opts []cmp.Option, want ...proto.Message) {
	p.t.Helper()

	diff := cmp.Diff(want, p.Sent(procedure), append([]cmp.Option{protocmp.Transform(), cmpopts.EquateEmpty()}, opts...)...)
	assert.Empty(p.t, diff, "the requests sent to %s (-want +got)", procedure)
}

// Sent is every request the panel has sent to a procedure.
func (p *Panel) Sent(procedure string) []proto.Message {
	var sent []proto.Message
	for _, seen := range p.Requests() {
		if seen.Procedure == procedure {
			sent = append(sent, seen.Msg)
		}
	}

	return sent
}

// Tmux is the tmux the panel drives.
func (p *Panel) Tmux() *recordedTmux {
	return p.tmux
}

// Copied is everything the panel has copied, in order.
func (p *Panel) Copied() []string {
	p.clip.mu.Lock()
	defer p.clip.mu.Unlock()

	return slices.Clone(p.clip.written)
}

// SetClipboardErr makes every later copy fail with err, or succeed while it is nil.
func (p *Panel) SetClipboardErr(err error) {
	p.clip.mu.Lock()
	defer p.clip.mu.Unlock()

	p.clip.err = err
}

// Edited is the file each editor the panel opened was opened on, in order.
func (p *Panel) Edited() []string {
	p.editor.mu.Lock()
	defer p.editor.mu.Unlock()

	return slices.Clone(p.editor.opened)
}

// IsWatching reports whether the panel holds its watch stream open.
func (p *Panel) IsWatching() bool {
	p.watcher.mu.Lock()
	defer p.watcher.mu.Unlock()

	return p.watcher.watching
}

func (w *watcher) Watch(onBatch func(client.WatchBatch), onLost func(error)) func() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.onBatch, w.onLost, w.watching = onBatch, onLost, true

	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()

		w.watching = false
	}
}

func (e *editor) Edit(path string, closed func(error) tea.Msg) tea.Cmd {
	e.mu.Lock()
	e.opened = append(e.opened, path)
	text := e.next
	e.next = nil
	e.mu.Unlock()

	return func() tea.Msg {
		if text == nil {
			return closed(nil)
		}

		return closed(os.WriteFile(path, []byte(*text), 0o600))
	}
}

func (c *clipboard) WriteAll(text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.written = append(c.written, text)

	return c.err
}
