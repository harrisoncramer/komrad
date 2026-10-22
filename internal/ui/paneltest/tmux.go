package paneltest

import (
	"fmt"
	"strconv"
	"strings"
	"sync"

	"komrad/internal/panes"
	"komrad/internal/ui"
	"komrad/lib/xslice"
)

// The panes the recorded tmux hands out, named for what the frame puts in them.
const (
	sidebarPane = "%0"
	StagePane   = "%1"
	InfoPane    = "%2"
	ColumnPane  = "%6"
)

// recordedTmux is the tmux a panel under test drives, recording every call and where each pane stands.
type recordedTmux struct {
	mu sync.Mutex
	// wrapped is the tmux each call goes on to, nil where the recording answers it.
	wrapped      ui.Tmux
	sidebar      string
	refuseSplits bool
	// rendered is the frame the panel last rendered, which tmux shows in the panel's own pane.
	rendered func() string

	panelWidth   int
	panelHeight  int
	resizes      []string
	path         string
	swaps        int
	windowSizes  map[string]string
	sessionOpts  map[string]string
	respawns     []string
	height       int
	width        int
	paneWidths   map[string]int
	windowWidth  int
	sentKeys     []string
	detaches     int
	killed       []string
	belowAnchors []string
	globals      map[string]string
	selected     []string
	// selectedUnswapped is every pane handed the keyboard before any swap moved it.
	selectedUnswapped []string
	swapped           map[string]bool
	respawnPanes      []string
	respawnDirs       []string
	deadPanes         map[string]bool
	zoomed            bool
	zooms             []bool
	moveBatches       [][]panes.Kind
	keysElsewhere     bool
	activePane        string
	rootBinds         map[string][]string
	tableBinds        map[string][]string
	shown             []panes.Rect
	screens           map[string]string
	captured          map[string]string
	standIns          []panes.CopyStandIn
	standInErr        error
	windowHooks       map[string]string

	// stage is the pane split off as the stage and not killed since.
	stage string
	// slot is the pane standing where the stage was split.
	slot string
	// sidebarWidth is the width the panel last gave its own pane.
	sidebarWidth int
}

func newTmux(wrapped ui.Tmux, sidebar string, width, height int) *recordedTmux {
	return &recordedTmux{
		wrapped:     wrapped,
		sidebar:     sidebar,
		width:       width,
		height:      height,
		windowWidth: width,
	}
}

// GuestPane is the live pane standing where the stage was split, other than the stage itself.
func (t *recordedTmux) GuestPane() string {
	t.mu.Lock()
	slot, stage := t.slot, t.stage
	t.mu.Unlock()

	if slot == "" || slot == stage || !t.IsPaneAlive(slot) {
		return ""
	}

	return slot
}

// StagePane is the pane split off as the stage, empty once it is killed.
func (t *recordedTmux) StagePane() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.stage
}

// SidebarWidth is the width the panel last gave its own pane.
func (t *recordedTmux) SidebarWidth() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.sidebarWidth
}

// StandIns is every stand-in the panel has put in a pane's place.
func (t *recordedTmux) StandIns() []panes.CopyStandIn {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.standIns
}

// RefuseStandIns makes tmux fail every stand-in with err.
func (t *recordedTmux) RefuseStandIns(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.standInErr = err
}

// SelectedPanes is every pane the panel has handed the keyboard to.
func (t *recordedTmux) SelectedPanes() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.selected
}

// SelectedUnswapped is every pane the panel handed the keyboard to before a swap had moved it.
func (t *recordedTmux) SelectedUnswapped() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.selectedUnswapped
}

// Respawns is every command the panel has respawned a pane with.
func (t *recordedTmux) Respawns() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.respawns
}

// RespawnPanes is every pane the panel has respawned, in the order it respawned them.
func (t *recordedTmux) RespawnPanes() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.respawnPanes
}

// RespawnDirs is the directory each respawn rooted at, in the order the panel asked for them.
func (t *recordedTmux) RespawnDirs() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.respawnDirs
}

// KilledPanes is every pane the panel has killed.
func (t *recordedTmux) KilledPanes() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.killed
}

// Detaches is how many times the panel has detached its session's clients.
func (t *recordedTmux) Detaches() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.detaches
}

// Resizes is every pane resize the panel has asked tmux for.
func (t *recordedTmux) Resizes() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.resizes
}

// WindowSizes is the size the panel last gave each agent's window.
func (t *recordedTmux) WindowSizes() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.windowSizes
}

// BelowAnchors is every pane the panel has split a pane under or over.
func (t *recordedTmux) BelowAnchors() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.belowAnchors
}

// RootBinding is the command a key is bound to in tmux's root table, and false while it is unbound.
func (t *recordedTmux) RootBinding(key string) ([]string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	command, bound := t.rootBinds[key]

	return command, bound
}

// TableBinding is the command a key runs in a named key table, and whether it is bound.
func (t *recordedTmux) TableBinding(table, key string) ([]string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	command, bound := t.tableBinds[table+":"+key]

	return command, bound
}

// SentKeys is every key send the panel has typed into a pane, its target first.
func (t *recordedTmux) SentKeys() []string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.sentKeys
}

// Swaps is how many times the panel has swapped two panes.
func (t *recordedTmux) Swaps() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.swaps
}

// IsZoomed reports whether the panel last asked tmux to zoom its pane over the window.
func (t *recordedTmux) IsZoomed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.zoomed
}

// Zooms is every zoom the panel has asked tmux for, in order.
func (t *recordedTmux) Zooms() []bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.zooms
}

// MoveBatches is the kinds of pane move in each invocation of tmux the panel sent, in order.
func (t *recordedTmux) MoveBatches() [][]panes.Kind {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.moveBatches
}

// SetPaneWidth makes tmux report a pane at the given width.
func (t *recordedTmux) SetPaneWidth(pane string, width int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.paneWidths == nil {
		t.paneWidths = map[string]int{}
	}

	t.paneWidths[pane] = width
}

// SetWindowWidth makes tmux report the window at the given width.
func (t *recordedTmux) SetWindowWidth(width int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.windowWidth = width
}

// SetActivePane makes tmux report the keyboard in pane, which is elsewhere unless it is the
// panel's own.
func (t *recordedTmux) SetActivePane(pane string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.activePane = pane
	t.keysElsewhere = pane != t.sidebar
}

// SetKeysElsewhere makes tmux report whether the keyboard is in a pane other than the panel's.
func (t *recordedTmux) SetKeysElsewhere(elsewhere bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.keysElsewhere = elsewhere
}

// SetScreens is what tmux shows in the panel's window, pane by pane.
func (t *recordedTmux) SetScreens(shown []panes.Rect, screens map[string]string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.shown, t.screens = shown, screens
}

// SetPaneSize makes tmux report every pane and the window at the given size.
func (t *recordedTmux) SetPaneSize(width, height int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.width, t.height, t.windowWidth = width, height, width
	delete(t.paneWidths, t.sidebar)
}

// SetPaneDead makes tmux report a pane as gone.
func (t *recordedTmux) SetPaneDead(pane string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.deadPanes == nil {
		t.deadPanes = map[string]bool{}
	}

	t.deadPanes[pane] = true
}

// SetHostPath makes tmux report the directory of the client the panel was invoked from.
func (t *recordedTmux) SetHostPath(path string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.path = path
}

// told records the size the terminal last told the panel its pane is.
func (t *recordedTmux) told(width, height int) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.panelWidth, t.panelHeight = width, height
	if t.wrapped != nil {
		return
	}

	if t.paneWidths == nil {
		t.paneWidths = map[string]int{}
	}

	t.paneWidths[t.sidebar] = width
	t.height = height
}

// captures is what the picker last captured of each pane.
func (t *recordedTmux) captures() map[string]string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.captured
}

func (t *recordedTmux) ShowGlobal(name string) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.globals[name]
}

func (t *recordedTmux) SetGlobal(name, value string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.globals == nil {
		t.globals = map[string]string{}
	}

	t.globals[name] = value
}

func (t *recordedTmux) SetSessionOption(target, name, value string) {
	t.mu.Lock()
	if t.sessionOpts == nil {
		t.sessionOpts = map[string]string{}
	}
	t.sessionOpts[name] = value
	t.mu.Unlock()

	if t.wrapped != nil {
		t.wrapped.SetSessionOption(target, name, value)
	}
}

func (t *recordedTmux) DetachSession(string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.detaches++
}

func (t *recordedTmux) TuneCopyMode(string) {}

func (t *recordedTmux) SetWindowHook(window, hook, command string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.windowHooks == nil {
		t.windowHooks = map[string]string{}
	}
	t.windowHooks[window+":"+hook] = command
}

func (t *recordedTmux) UnsetWindowHook(window, hook string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.windowHooks, window+":"+hook)
}

func (t *recordedTmux) OpenCopyStandIn(params panes.CopyStandIn) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.standIns = append(t.standIns, params)

	return t.standInErr
}

func (t *recordedTmux) BindRootKey(key string, command ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.rootBinds == nil {
		t.rootBinds = map[string][]string{}
	}

	t.rootBinds[key] = command
}

func (t *recordedTmux) UnbindRootKey(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.rootBinds, key)
}

func (t *recordedTmux) BindTableKey(table, key string, command ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tableBinds == nil {
		t.tableBinds = map[string][]string{}
	}

	t.tableBinds[table+":"+key] = command
}

func (t *recordedTmux) BindRepeatTableKey(table, key string, command ...string) {
	t.BindTableKey(table, key, command...)
}

func (t *recordedTmux) UnbindTable(table string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for bound := range t.tableBinds {
		if strings.HasPrefix(bound, table+":") {
			delete(t.tableBinds, bound)
		}
	}
}

func (t *recordedTmux) GetWindowID(string) string {
	return "@1"
}

func (t *recordedTmux) GetHostPath() string {
	if t.wrapped != nil {
		return t.wrapped.GetHostPath()
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	return t.path
}

func (t *recordedTmux) GetDefaultShell() string {
	if t.wrapped != nil {
		return t.wrapped.GetDefaultShell()
	}

	return "/bin/zsh"
}

func (t *recordedTmux) SendKeys(target string, keys ...string) error {
	t.mu.Lock()
	t.sentKeys = append(t.sentKeys, strings.Join(append([]string{target}, keys...), " "))
	t.mu.Unlock()

	if t.wrapped != nil {
		return t.wrapped.SendKeys(target, keys...)
	}

	return nil
}

// MovePanes records a change's pane moves one by one, so a story asserting on swaps and zooms sees
// what the frame asked for whether or not they were collected first.
func (t *recordedTmux) MovePanes(moves []panes.Move) {
	t.mu.Lock()
	t.moveBatches = append(t.moveBatches, xslice.Map(moves, func(move panes.Move) panes.Kind { return move.Kind }))
	for _, move := range moves {
		switch move.Kind {
		case panes.KindZoom:
			t.recordZoom(move.Zoomed)
		case panes.KindSwap:
			t.recordSwap(move.Pane, move.Other)
		case panes.KindRespawn:
			t.recordRespawn(move.Pane, move.Dir, move.Argv)
		case panes.KindResize:
			t.recordResize(move.Pane, strconv.Itoa(move.Width))
		case panes.KindKill:
			t.recordKill(move.Pane)
		}
	}
	t.mu.Unlock()

	if t.wrapped != nil {
		t.wrapped.MovePanes(moves)
	}
}

func (t *recordedTmux) IsPaneAlive(pane string) bool {
	if t.wrapped != nil {
		return t.wrapped.IsPaneAlive(pane)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	return !t.deadPanes[pane]
}

func (t *recordedTmux) GetWindowWidth(pane string) int {
	if t.wrapped != nil {
		return t.wrapped.GetWindowWidth(pane)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	return t.windowWidth
}

func (t *recordedTmux) GetPaneWidth(pane string) int {
	if t.wrapped != nil {
		return t.wrapped.GetPaneWidth(pane)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if width, ok := t.paneWidths[pane]; ok {
		return width
	}

	return t.width
}

func (t *recordedTmux) GetPaneHeight(pane string) int {
	if t.wrapped != nil {
		return t.wrapped.GetPaneHeight(pane)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	return t.height
}

// GetPaneFocus answers the way tmux was measured to: a pane the keys have moved off is neither
// the active one nor zoomed any more.
func (t *recordedTmux) GetPaneFocus(pane string) (focused, zoomed bool) {
	if t.wrapped != nil {
		return t.wrapped.GetPaneFocus(pane)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.keysElsewhere {
		return false, false
	}

	return true, t.zoomed
}

// SplitRight answers the stage, and the column for a pane split off while the stage stands.
func (t *recordedTmux) SplitRight(anchor, dir, size string) string {
	if t.refuseSplits {
		return ""
	}

	pane := ColumnPane
	t.mu.Lock()
	staging := t.stage == ""
	if staging {
		pane = StagePane
	}
	t.mu.Unlock()

	if t.wrapped != nil {
		pane = t.wrapped.SplitRight(anchor, dir, size)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.zoomed = false
	if staging && pane != "" {
		t.stage, t.slot = pane, pane
	}
	if cols, err := strconv.Atoi(size); err == nil && pane != "" && anchor == t.sidebar && t.sidebarWidth > cols {
		t.sidebarWidth -= cols + 1
	}

	return pane
}

func (t *recordedTmux) SplitBelow(anchor, dir, size string) string {
	t.mu.Lock()
	t.belowAnchors = append(t.belowAnchors, anchor)
	t.zoomed = false
	t.mu.Unlock()

	if t.wrapped != nil {
		return t.wrapped.SplitBelow(anchor, dir, size)
	}

	return InfoPane
}

func (t *recordedTmux) SplitAbove(anchor, dir, size string) string {
	t.mu.Lock()
	t.belowAnchors = append(t.belowAnchors, anchor)
	t.zoomed = false
	t.mu.Unlock()

	if t.wrapped != nil {
		return t.wrapped.SplitAbove(anchor, dir, size)
	}

	return InfoPane
}

func (t *recordedTmux) SetInfoPane(pane string) {
	if t.wrapped != nil {
		t.wrapped.SetInfoPane(pane)
	}
}

func (t *recordedTmux) GetActivePane(pane string) string {
	if t.wrapped != nil {
		return t.wrapped.GetActivePane(pane)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activePane != "" {
		return t.activePane
	}

	if t.keysElsewhere {
		return ""
	}

	return pane
}

func (t *recordedTmux) SelectPane(pane string) {
	t.mu.Lock()
	t.selected = append(t.selected, pane)
	if !t.swapped[pane] {
		t.selectedUnswapped = append(t.selectedUnswapped, pane)
	}
	t.mu.Unlock()

	if t.wrapped != nil {
		t.wrapped.SelectPane(pane)
	}
}

func (t *recordedTmux) GetSiblingPanes(pane string) []string {
	if t.wrapped != nil {
		return t.wrapped.GetSiblingPanes(pane)
	}

	return nil
}

func (t *recordedTmux) GetPaneOwner(pane string) string {
	if t.wrapped != nil {
		return t.wrapped.GetPaneOwner(pane)
	}

	return ""
}

func (t *recordedTmux) BreakPane(pane string) {
	t.mu.Lock()
	t.zoomed = false
	if t.slot == pane {
		t.slot = ""
	}
	t.mu.Unlock()

	if t.wrapped != nil {
		t.wrapped.BreakPane(pane)
	}
}

func (t *recordedTmux) KillPane(pane string) {
	t.mu.Lock()
	t.recordKill(pane)
	t.mu.Unlock()

	if t.wrapped != nil {
		t.wrapped.KillPane(pane)
	}
}

func (t *recordedTmux) NewWindow(string, string, string) (string, error) {
	return InfoPane, nil
}

// ResizeWindow records the size an agent's own window was put at, keyed by pane.
func (t *recordedTmux) ResizeWindow(pane string, width, height int) {
	t.mu.Lock()
	if t.windowSizes == nil {
		t.windowSizes = map[string]string{}
	}
	t.windowSizes[pane] = fmt.Sprintf("%dx%d", width, height)
	t.mu.Unlock()

	if t.wrapped != nil {
		t.wrapped.ResizeWindow(pane, width, height)
	}
}

func (t *recordedTmux) IsPaneRunningCLI(pane string) bool {
	if t.wrapped != nil {
		return t.wrapped.IsPaneRunningCLI(pane)
	}

	return pane != ""
}

func (t *recordedTmux) ResizePane(pane, width string) {
	t.mu.Lock()
	t.recordResize(pane, width)
	t.mu.Unlock()

	if t.wrapped != nil {
		t.wrapped.ResizePane(pane, width)
	}
}

func (t *recordedTmux) GetShownPanes(sidebar string) []panes.Rect {
	if t.wrapped != nil {
		return t.wrapped.GetShownPanes(sidebar)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.shown == nil {
		return []panes.Rect{{Pane: sidebar, Width: t.panelWidth, Height: t.panelHeight}}
	}

	return t.shown
}

// CaptureScreen answers what each pane shows, the panel's own showing the frame it last rendered.
func (t *recordedTmux) CaptureScreen(pane string) string {
	var screen string
	switch {
	case t.wrapped != nil:
		screen = t.wrapped.CaptureScreen(pane)
	case t.screens != nil:
		screen = t.screens[pane]
	case pane == t.sidebar && t.rendered != nil:
		screen = t.rendered()
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.captured == nil {
		t.captured = map[string]string{}
	}
	t.captured[pane] = screen

	return screen
}

func (t *recordedTmux) recordZoom(on bool) {
	t.zoomed = on
	t.zooms = append(t.zooms, on)
}

func (t *recordedTmux) recordSwap(pane, other string) {
	t.swaps++

	if t.swapped == nil {
		t.swapped = map[string]bool{}
	}

	t.swapped[pane] = true
	t.swapped[other] = true
	t.zoomed = false

	switch t.slot {
	case pane:
		t.slot = other
	case other:
		t.slot = pane
	}
}

func (t *recordedTmux) recordRespawn(pane, dir string, argv []string) {
	t.respawns = append(t.respawns, strings.Join(argv, " "))
	t.respawnPanes = append(t.respawnPanes, pane)
	t.respawnDirs = append(t.respawnDirs, dir)
}

func (t *recordedTmux) recordResize(pane, width string) {
	t.zoomed = false
	t.resizes = append(t.resizes, pane+" "+width)

	if cols, err := strconv.Atoi(width); err == nil && pane == t.sidebar {
		t.sidebarWidth = cols
	}
}

func (t *recordedTmux) recordKill(pane string) {
	t.killed = append(t.killed, pane)
	t.zoomed = false

	if t.stage == pane {
		t.stage = ""
	}
	if t.slot == pane {
		t.slot = ""
	}
}
