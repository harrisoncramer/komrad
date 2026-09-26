package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/browser"
	"komrad/internal/engine"
	"komrad/internal/panes"
	"komrad/internal/ui/layout"
	"komrad/lib/xslice"
	"komrad/proto/gen/agent/v1/agentv1connect"
	"komrad/proto/gen/hook/v1/hookv1connect"
	schedulev1connect "komrad/proto/gen/schedule/v1/schedulev1connect"
	"komrad/proto/gen/workflow/v1/workflowv1connect"
	"komrad/proto/gen/worktree/v1/worktreev1connect"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const panelViewer = "/dev/ttys004"

var updateGoldens = flag.Bool("update", false, "rewrite the golden files")

// FormFieldDraftPath is the file a form field is written out to for the editor.
var FormFieldDraftPath = formFieldDraftPath

// Panel is the panel's model driven the way the running program drives it.
type Panel struct {
	model model
	post  *postbox
	tmux  *recordingTmux
	clip  *recordingClipboard
}

type unsplittableTmux struct {
	*recordingTmux
}

type recordingTmux struct {
	resizes     []string
	path        string
	swaps       int
	windowSizes map[string]string
	sessionOpts map[string]string
	respawns    []string
	width       int
	height      int
	// paneWidths is one pane's own width, falling back to width where a pane has no entry.
	paneWidths    map[string]int
	windowWidth   int
	sentKeys      []string
	detaches      int
	killed        []string
	belowAnchors  []string
	globals       map[string]string
	infoPane      string
	selected      []string
	respawnPanes  []string
	respawnDirs   []string
	deadPanes     map[string]bool
	sessionClient string
	zoomed        bool
	// keysElsewhere is the keyboard sitting in a pane other than the one being asked about.
	keysElsewhere bool
	// activePane is the pane the window sends keys to, where a test has put them somewhere named.
	activePane    string
	rootBinds     map[string][]string
	rootBindCount int
	rootUnbinds   []string
}

// PanelTmux is the tmux a panel under test drives in place of the recording one.
type PanelTmux = uiTmux

type recordingClipboard struct {
	written []string
	err     error
}

// HarnessFrameReader answers whether a pane is drawing its harness yet.
type HarnessFrameReader = harnessFrameReader

// NewPanelParams is what a panel under test is built over.
type NewPanelParams struct {
	Agents          agentv1connect.AgentServiceClient
	Workflows       workflowv1connect.WorkflowServiceClient
	Schedules       schedulev1connect.ScheduleServiceClient
	Hooks           hookv1connect.HookServiceClient
	Worktrees       worktreev1connect.WorktreeServiceClient
	Git             ReviewGit
	Keys            config.Keybindings
	DetailsHeight   float64
	SidebarSide     string
	SidebarWidth    int
	Width           int
	Height          int
	Settings        PanelSettings
	Engine          engine.Remote
	ShellCommand    []string
	TerminalCommand []string
	HarnessFrames   harnessFrameReader
	ConfigErr       error
	StartupErr      error
	RefuseSplits    bool
	Binary          BinaryWatcher
	Browser         browser.Opener
	Tmux            PanelTmux
}

// NewPanel builds the panel the way komrad panel does, over a recording tmux.
func NewPanel(params NewPanelParams) *Panel {
	settings := params.Settings
	if settings.Pane == "" {
		settings.Pane = "%0"
	}
	tmux := &recordingTmux{
		width:       params.Width,
		height:      params.Height,
		windowWidth: params.Width,
	}
	var driven uiTmux = tmux
	if params.RefuseSplits {
		driven = unsplittableTmux{tmux}
	}
	if params.Tmux != nil {
		driven = params.Tmux
	}
	post := newPostbox()
	layout := config.DefaultLayout()
	if params.DetailsHeight != 0 {
		layout.DetailsHeight = params.DetailsHeight
	}
	if params.SidebarSide != "" {
		layout.SidebarSide = params.SidebarSide
	}
	if params.SidebarWidth != 0 {
		layout.SidebarWidth = params.SidebarWidth
	}

	panel := &Panel{
		model: newModel(Deps{
			Session:         NewSession(tmux, "test-session", spellCycleKeys(params.Keys.Cycle.Views)),
			Tmux:            driven,
			Git:             params.Git,
			Agents:          params.Agents,
			Workflows:       params.Workflows,
			Schedules:       params.Schedules,
			Hooks:           params.Hooks,
			Worktrees:       params.Worktrees,
			Keys:            params.Keys,
			Layout:          layout,
			Engine:          params.Engine,
			ShellCommand:    params.ShellCommand,
			TerminalCommand: params.TerminalCommand,
			HarnessFrames:   params.HarnessFrames,
			Settings:        settings,
			ConfigErr:       params.ConfigErr,
			StartupErr:      params.StartupErr,
			Binary:          params.Binary,
			Browser:         params.Browser,
		}, true, post),
		post: post,
		tmux: tmux,
		clip: &recordingClipboard{},
	}
	panel.model.clip = panel.clip

	return panel
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

	read := func() tea.Msg {
		return sizeReadMsg{size: *size}
	}
	if size.Width == p.model.paneCols && size.Height == p.model.height {
		return tea.Batch(cmd, read)
	}

	resized := func() tea.Msg {
		return *size
	}

	return tea.Batch(cmd, read, resized)
}

// View is the frame the panel draws.
func (p *Panel) View() string {
	return p.model.render()
}

// SidebarCols is the columns the list is drawn in beside a block at the default sidebar width.
const SidebarCols = config.DefaultSidebarWidth - dividerCols

// Inside is the columns left of the frame the panel draws inside its gutter and edge, and how wide
// that frame is.
func (p *Panel) Inside() (int, int) {
	return p.model.countInsetLeft(), p.model.width
}

// NewKeyPress is the press bubbletea delivers for a key as a binding spells it.
func NewKeyPress(key string) tea.KeyPressMsg {
	return keyMsg(key)
}

// NewWatchLost is the message the panel hears when its watch stream ends.
func NewWatchLost(err error) tea.Msg {
	return watchLostMsg{err: err}
}

// RequireGolden compares a drawn block against the file named for it under testdata.
func RequireGolden(t *testing.T, name, got string) {
	t.Helper()

	requireGolden(t, name, got)
}

// NewTick is the refresh loop's tick as it lands at the given time.
func NewTick(at time.Time) tea.Msg {
	return tickMsg{at: at}
}

// NewFieldEdited is what the editor hands back for a field of the form on screen once it quits.
func (p *Panel) NewFieldEdited(key, text string) tea.Msg {
	return formFieldEditedMsg{
		kind: p.model.form.kind,
		key:  key,
		text: text,
	}
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

// ReadResizes is every pane resize the panel has asked tmux for.
func (p *Panel) ReadResizes() []string {
	return p.tmux.resizes
}

// ReadWindowSizes is the size the panel last gave each agent's window.
func (p *Panel) ReadWindowSizes() map[string]string {
	return p.tmux.windowSizes
}

// ReadCopied is everything the panel has put on the clipboard.
func (p *Panel) ReadCopied() []string {
	return p.clip.written
}

// ReadRespawnPanes is every pane the panel has respawned, in the order it respawned them.
func (p *Panel) ReadRespawnPanes() []string {
	return p.tmux.respawnPanes
}

// ReadRespawnDirs is the directory each respawn was rooted at, in the order it was asked for.
func (p *Panel) ReadRespawnDirs() []string {
	return p.tmux.respawnDirs
}

// ReadBelowAnchors is every pane the panel has split a pane under.
func (p *Panel) ReadBelowAnchors() []string {
	return p.tmux.belowAnchors
}

// ReadRootBinding is the command a key is bound to in tmux's root table, and false while it is unbound.
func (p *Panel) ReadRootBinding(key string) ([]string, bool) {
	command, bound := p.tmux.rootBinds[key]

	return command, bound
}

// ReadRootUnbinds is every key the panel has taken out of tmux's root table.
func (p *Panel) ReadRootUnbinds() []string {
	return p.tmux.rootUnbinds
}

// ReadGlobalArmKeys is every key the keybinding check expects the router to answer ahead of the screen that is up.
func (p *Panel) ReadGlobalArmKeys() []string {
	var keys []string
	for _, arm := range globalArms(p.model.screen, p.model.keys) {
		keys = append(keys, arm.keys...)
	}

	return keys
}

// ReadLegendKeys is every key the legend of the screen that is up names.
func (p *Panel) ReadLegendKeys() []string {
	shared := p.model.shared()

	var legend keyMap
	switch p.model.screen {
	case screenAgents:
		legend = p.model.listHelp(p.model.agents.help(shared))
	case screenSchedules:
		legend = p.model.listHelpOf(p.model.schedules.listParams(shared))
	case screenArchive:
		legend = p.model.listHelpOf(p.model.archive.listParams(shared))
	case screenSearch:
		legend = p.model.search.help(shared)
	case screenReview:
		legend = p.model.review.help(shared)
	case screenWorktrees:
		legend = p.model.listHelpOf(p.model.worktrees.listParams(shared))
	}

	var keys []string
	for _, binding := range slices.Concat(legend.short, legend.full) {
		keys = append(keys, binding.Keys()...)
	}

	return keys
}

// ReadStagePane is the pane the panel's frame holds as its stage.
func (p *Panel) ReadStagePane() string {
	return p.model.frame.Pane(layout.RegionStage)
}

// CountRespawns is how many panes the panel has respawned.
func (p *Panel) CountRespawns() int {
	return len(p.tmux.respawns)
}

// CountSentKeys is how many key sends the panel has typed into panes.
func (p *Panel) CountSentKeys() int {
	return len(p.tmux.sentKeys)
}

// CountSwaps is how many times the panel has swapped two panes.
func (p *Panel) CountSwaps() int {
	return p.tmux.swaps
}

// CountRootBinds is how many times the panel has bound a key in tmux's root table.
func (p *Panel) CountRootBinds() int {
	return p.tmux.rootBindCount
}

// IsZoomed reports whether the panel last asked tmux to zoom its pane over the window.
func (p *Panel) IsZoomed() bool {
	return p.tmux.zoomed
}

// IsKeyTaken reports whether a press of the key reaches any binding the panel dispatches on.
func (p *Panel) IsKeyTaken(key string) bool {
	isTaken := false
	keyProbe = func([]string) {
		isTaken = true
	}
	defer func() {
		keyProbe = nil
	}()

	p.model.route(keyMsg(key))

	return isTaken
}

// IsGlobalKey reports whether the router answers a press of the key ahead of the screen that is up.
func (p *Panel) IsGlobalKey(key string) bool {
	_, _, isTaken := p.model.routeGlobalKey(keyMsg(key))

	return isTaken
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

// SetActivePane makes tmux report the keyboard in pane, which is elsewhere unless it is the
// panel's own.
func (p *Panel) SetActivePane(pane string) {
	p.tmux.activePane = pane
	p.tmux.keysElsewhere = pane != p.model.settings.Pane
}

// SetKeysElsewhere makes tmux report whether the keyboard is in a pane other than the panel's.
func (p *Panel) SetKeysElsewhere(elsewhere bool) {
	p.tmux.keysElsewhere = elsewhere
}

// SetPaneSize makes tmux report every pane and the window at the given size.
func (p *Panel) SetPaneSize(width, height int) {
	p.tmux.width, p.tmux.height, p.tmux.windowWidth = width, height, width
}

// SetClipboardErr makes every later copy fail with err, or succeed while it is nil.
func (p *Panel) SetClipboardErr(err error) {
	p.clip.err = err
}

// SetPaneDead makes tmux report a pane as gone.
func (p *Panel) SetPaneDead(pane string) {
	if p.tmux.deadPanes == nil {
		p.tmux.deadPanes = map[string]bool{}
	}

	p.tmux.deadPanes[pane] = true
}

// SetShown makes tmux report whether a client is looking at the panel's session.
func (p *Panel) SetShown(isShown bool) {
	p.tmux.sessionClient = ""
	if isShown {
		p.tmux.sessionClient = panelViewer
	}
}

// SetHostPath makes tmux report the directory of the client the panel was invoked from.
func (p *Panel) SetHostPath(path string) {
	p.tmux.path = path
}

// ExpireLanding runs out the wait a write's landing holds the cursor for.
func (p *Panel) ExpireLanding() {
	p.model.landing.until = time.Time{}
}

// AgeWorktreeReads makes every directory read the panel holds older by the given time.
func (p *Panel) AgeWorktreeReads(by time.Duration) {
	for dir, read := range p.model.inspections {
		read.readAt = read.readAt.Add(-by)
		p.model.inspections[dir] = read
	}
}

func requireGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")
	if *updateGoldens {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))

		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden file yet, so run the suite with -update")
	assert.Equal(t, string(want), got)
}

func (unsplittableTmux) SplitRight(string, string, string) string {
	return ""
}

func (unsplittableTmux) SplitLeft(string, string, string) string {
	return ""
}

func (t *recordingTmux) ShowGlobal(name string) string {
	return t.globals[name]
}

func (t *recordingTmux) GetSessionClient(string) string {
	return t.sessionClient
}

func (t *recordingTmux) SetSessionOption(_, name, value string) {
	if t.sessionOpts == nil {
		t.sessionOpts = map[string]string{}
	}

	t.sessionOpts[name] = value
}

func (t *recordingTmux) DetachSession(string) {
	t.detaches++
}

func (t *recordingTmux) BindRootKey(key string, command ...string) {
	if t.rootBinds == nil {
		t.rootBinds = map[string][]string{}
	}

	t.rootBinds[key] = command
	t.rootBindCount++
}

func (t *recordingTmux) UnbindRootKey(key string) {
	delete(t.rootBinds, key)
	t.rootUnbinds = append(t.rootUnbinds, key)
}

func (t *recordingTmux) GetHostPath() string {
	return t.path
}

func (t *recordingTmux) GetDefaultShell() string {
	return "/bin/zsh"
}

func (t *recordingTmux) SendKeys(target string, keys ...string) error {
	t.sentKeys = append(t.sentKeys, strings.Join(append([]string{target}, keys...), " "))

	return nil
}

// MovePanes replays a change's pane moves onto the fake, so a test asserting on swaps and
// zooms sees what the frame asked for whether or not they were collected first.
func (t *recordingTmux) MovePanes(moves []panes.Move) {
	for _, move := range moves {
		switch move.Kind {
		case panes.KindZoom:
			t.SetPaneZoomed(move.Pane, move.Zoomed)
		case panes.KindSwap:
			t.SwapPanes(move.Pane, move.Other)
		case panes.KindRespawn:
			t.RespawnPane(move.Pane, move.Dir, move.Argv...)
		case panes.KindResize:
			t.ResizePane(move.Pane, strconv.Itoa(move.Width))
		}
	}
}

func (t *recordingTmux) IsPaneAlive(pane string) bool {
	return !t.deadPanes[pane]
}

func (t *recordingTmux) SetPaneZoomed(_ string, on bool) {
	t.zoomed = on
}

func (t *recordingTmux) GetWindowWidth(string) int {
	return t.windowWidth
}

func (t *recordingTmux) GetPaneWidth(pane string) int {
	if width, ok := t.paneWidths[pane]; ok {
		return width
	}

	return t.width
}

// GetPaneFocus answers the way tmux was measured to: a pane the keys have moved off is neither
// the active one nor zoomed any more.
func (t *recordingTmux) GetPaneFocus(string) (focused, zoomed bool) {
	if t.keysElsewhere {
		return false, false
	}

	return true, t.zoomed
}

func (t *recordingTmux) GetPaneHeight(string) int {
	return t.height
}

func (t *recordingTmux) SplitRight(string, string, string) string {
	return t.split("%1")
}

func (t *recordingTmux) SplitLeft(string, string, string) string {
	return t.split("%1")
}

func (t *recordingTmux) SplitBelow(anchor, _, _ string) string {
	t.belowAnchors = append(t.belowAnchors, anchor)

	return t.split("%2")
}

func (t *recordingTmux) SplitAbove(anchor, _, _ string) string {
	t.belowAnchors = append(t.belowAnchors, anchor)

	return t.split("%2")
}

// split answers with the pane a split left, dropping the zoom the way tmux does.
func (t *recordingTmux) split(pane string) string {
	t.zoomed = false

	return pane
}

func (t *recordingTmux) SetInfoPane(pane string) {
	t.infoPane = pane
}

func (t *recordingTmux) GetActivePane(pane string) string {
	if t.activePane != "" {
		return t.activePane
	}

	if t.keysElsewhere {
		return ""
	}

	return pane
}

func (t *recordingTmux) SelectPane(pane string) {
	t.selected = append(t.selected, pane)
}

func (t *recordingTmux) SetGlobal(name, value string) {
	if t.globals == nil {
		t.globals = map[string]string{}
	}

	t.globals[name] = value
}

func (t *recordingTmux) GetSiblingPanes(string) []string {
	return nil
}

func (t *recordingTmux) GetPaneOwner(string) string {
	return ""
}

// SwapPanes, BreakPane, KillPane and ResizePane each drop the zoom, the way tmux does.
func (t *recordingTmux) SwapPanes(string, string) {
	t.swaps++
	t.zoomed = false
}

func (t *recordingTmux) BreakPane(string) {
	t.zoomed = false
}

func (t *recordingTmux) KillPane(pane string) {
	t.killed = append(t.killed, pane)
	t.zoomed = false
}

func (t *recordingTmux) NewWindow(string, string, string) (string, error) {
	return "%2", nil
}

// ResizeWindow records the size an agent's own window was put at, keyed by pane.
func (t *recordingTmux) ResizeWindow(pane string, width, height int) {
	if t.windowSizes == nil {
		t.windowSizes = map[string]string{}
	}

	t.windowSizes[pane] = fmt.Sprintf("%dx%d", width, height)
}

func (t *recordingTmux) IsPaneRunningCLI(pane string) bool {
	return pane != ""
}

func (t *recordingTmux) ResizePane(pane, width string) {
	t.zoomed = false
	t.resizes = append(t.resizes, pane+" "+width)
}

func (t *recordingTmux) RespawnPane(pane, dir string, argv ...string) {
	t.respawns = append(t.respawns, strings.Join(argv, " "))
	t.respawnPanes = append(t.respawnPanes, pane)
	t.respawnDirs = append(t.respawnDirs, dir)
}

func (f *recordingClipboard) WriteAll(text string) error {
	f.written = append(f.written, text)

	return f.err
}

func spellCycleKeys(keys []string) []string {
	return xslice.Map(keys, strings.NewReplacer("ctrl+", "C-", "alt+", "M-").Replace)
}
