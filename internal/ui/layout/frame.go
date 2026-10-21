package layout

import (
	"os"
	"slices"
	"strconv"
	"strings"

	"komrad/config"
	"komrad/internal/engine"
	"komrad/internal/panes"
)

// besidePercent is the percent of the columns right of the sidebar the sidebar takes while the stage
// is beside it, leaving the rest to the stage.
const besidePercent = 65

type Frame struct {
	tmux    Tmux
	spec    spec
	sidebar string
	stage   string
	info    string
	guest   string
	solo    bool

	// keys is whether the keyboard was on the panel's own pane at the last sync.
	keys bool

	// stageCols is how wide the pane beside the sidebar was when the divider was last measured,
	// which is where the sidebar's own width comes from: zooming the sidebar reports it as the
	// whole window while leaving the width of the pane beside it alone.
	stageCols int

	// cols is the width the sidebar was last settled at, by a drag or a nudge, zero until either.
	cols int

	// owed says the split under the zoom is behind cols.
	owed bool

	// pending holds the pane moves of the change being worked out, which Flush sends together and the
	// frame sends one at a time while nothing is collecting them.
	pending []panes.Move
	// batches is how many Begins are still waiting on their Flush.
	batches int
	shown   map[region]shown

	// zoomed says the change being collected moves the sidebar's zoom.
	zoomed bool

	// onSize is told the size the sidebar landed at, once the moves that changed it have been
	// sent.
	onSize func(width, height int)

	// remote is the engine the agents are running in, the zero value meaning this machine.
	remote engine.Remote
	// shellCommand is what opens a shell on the engine once given a directory, empty when the
	// engine is this machine.
	shellCommand []string
	// terminalCommand is what attaches to an agent's terminal on the engine once given the agent,
	// empty when the engine is this machine.
	terminalCommand []string

	// frozen is whether the window is held still, with held the moves it is keeping back until it thaws.
	frozen bool
	held   []panes.Move

	// remoteGuest is the agent whose terminal the stage is attached to, on a server this one does
	// not hold.
	remoteGuest string

	// folded is whether the stage gave its rows to the info pane for want of anything to show.
	folded bool

	// beside is whether the sidebar is drawn across part of the stage, with the stage beside it.
	beside bool

	// column is the pane split off the columns the sidebar took from the stage, empty while none is
	// open.
	column string
}

type shown struct {
	key    string
	width  int
	height int
}

// Guest is an agent whose terminal the stage shows.
type Guest struct {
	Agent string
	Pane  string
}

// option is a setting New takes beyond the geometry, so the common case names none of them.
type option func(*Frame)

// WithEngine says the agents are running somewhere this tmux server cannot reach.
func WithEngine(remote engine.Remote) option {
	return func(f *Frame) { f.remote = remote }
}

// WithShellCommand says what opens a shell on the engine, before the directory it roots in.
func WithShellCommand(command []string) option {
	return func(f *Frame) { f.shellCommand = command }
}

// WithTerminalCommand says what attaches to an agent's terminal on the engine, before the agent
// it is given.
func WithTerminalCommand(command []string) option {
	return func(f *Frame) { f.terminalCommand = command }
}

// New builds the frame the panel lays out around itself, at the geometry the config asks for.
func New(tmux Tmux, layout config.Layout, options ...option) *Frame {
	frame := &Frame{
		tmux:  tmux,
		spec:  newSpec(layout),
		shown: map[region]shown{},
	}

	for _, option := range options {
		option(frame)
	}

	return frame
}

// OnResize says where the frame reports the sidebar's new size once a change has moved it.
func (f *Frame) OnResize(report func(width, height int)) {
	f.onSize = report
}

// Attach anchors the frame to the pane the panel was told it runs in, never one read back from
// tmux, which answers for whatever window the session is looking at.
func (f *Frame) Attach(sidebar string) {
	f.sidebar = sidebar
	f.reclaim()
	f.claimStage()

	// A panel that died holding the window leaves the zoom behind it, and the next one starts on
	// a screen that never asked for it.
	f.Solo(false)
}

// Pane is the pane the frame holds for a region, which for the stage is its own pane even while
// a guest swaps into it.
func (f *Frame) Pane(region region) string {
	switch region {
	case RegionSidebar:
		return f.sidebar
	case RegionStage:
		return f.stage
	case RegionInfo:
		return f.info
	case regionNone:
	}

	return ""
}

// GuestPane is the pane somebody else owns currently swapped into the stage.
func (f *Frame) GuestPane() string {
	return f.guest
}

// FocusPane is the pane the focus key jumps to: the agent's own pane when it was moved into the
// stage and the stage itself when the stage is attached to an agent elsewhere.
func (f *Frame) FocusPane() string {
	if f.remote.IsRemote() {
		if f.remoteGuest == "" {
			return ""
		}

		return f.stage
	}

	return f.guest
}

// Begin collects the frame's pane moves for Flush to send together, so a change that moves
// several panes is one invocation of tmux and so one repaint rather than one for each.
func (f *Frame) Begin() {
	f.batches++
}

// Flush ends a Begin, sending the collected pane moves once the outermost one ends.
func (f *Frame) Flush() {
	if f.batches > 0 {
		f.batches--
	}

	if f.batches == 0 {
		f.send()
	}
}

// send sends the pending pane moves, and does nothing when there are none.
func (f *Frame) send() {
	moves, zoomed := f.pending, f.zoomed
	f.pending, f.zoomed = nil, false

	if len(moves) == 0 {
		return
	}

	f.tmux.MovePanes(moves)

	if zoomed {
		f.reportSize()
	}
}

// reportSize reads the sidebar back off the layout the moves left and hands the size on, which is
// the one moment it is worth asking tmux.
func (f *Frame) reportSize() {
	if f.onSize == nil {
		return
	}

	width, height := f.sidebarSize()

	if width <= 0 || height <= 0 {
		return
	}

	f.onSize(width, height)
}

// move records a pane move, sending it at once unless a change is being collected around it.
func (f *Frame) move(m panes.Move) {
	if f.frozen {
		f.held = append(f.held, m)

		return
	}

	f.pending = append(f.pending, m)

	if m.Kind == panes.KindZoom {
		f.zoomed = true
	}

	if f.batches == 0 {
		f.send()
	}
}

// Open opens a region and returns the pane it landed in, opening the stage on the way since
// every region hangs off it.
func (f *Frame) Open(region region) string {
	defer f.holdSolo()

	return f.open(region)
}

func (f *Frame) open(region region) string {
	switch region {
	case RegionStage:
		f.claimStage()
	case RegionInfo:
		if !f.isFolded() {
			f.claimStage()
		}
		f.claimInfo()
	case RegionSidebar, regionNone:
	}

	return f.Pane(region)
}

// IsOpen reports whether a region's pane is still running.
func (f *Frame) IsOpen(region region) bool {
	pane := f.Pane(region)

	return pane != "" && f.tmux.IsPaneAlive(pane)
}

// Close takes a region away.
func (f *Frame) Close(region region) {
	if region == RegionInfo {
		f.closeInfo()
		f.holdSolo()
	}
}

// Claimed records that another process has taken a region over, so the frame renders the next
// content rather than skipping it as already shown.
func (f *Frame) Claimed(region region) {
	f.forget(region)
}

// Show renders content in a region, opening it first when needed, and refuses the sidebar since
// filling it would replace the panel itself.
func (f *Frame) Show(region region, content Content) {
	if region != RegionStage && region != RegionInfo {
		return
	}

	defer f.holdSolo()

	f.open(region)

	if region == RegionStage {
		f.restoreGuest()
	}

	pane := f.Pane(region)
	if pane == "" {
		return
	}

	target := Target{Region: region}
	if content.Wraps {
		target.Width = f.tmux.GetPaneWidth(pane)
		target.Height = f.tmux.GetPaneHeight(pane)
	}

	if f.isShowing(region, content, target) {
		return
	}

	argv, ok := content.Render(target)
	if !ok {
		return
	}

	if region == RegionStage {
		f.remoteGuest = ""
	}

	f.move(panes.Respawn(pane, content.Dir, argv...))
	f.shown[region] = shown{
		key:    content.Key,
		width:  target.Width,
		height: target.Height,
	}
}

// ClearStage empties the stage, handing its rows to the info pane while one is open.
func (f *Frame) ClearStage() {
	if !f.IsOpen(RegionInfo) {
		f.Show(RegionStage, blankContent())

		return
	}

	if f.folded {
		return
	}

	f.restoreGuest()
	if f.tmux.IsPaneAlive(f.stage) {
		f.move(panes.Kill(f.stage))
	}
	f.stage, f.remoteGuest, f.folded = "", "", true
	f.forget(RegionStage)
}

func blankContent() Content {
	return Content{
		Key:    "blank",
		Dir:    getHomeDir(),
		Render: func(Target) ([]string, bool) { return nil, true },
	}
}

func (f *Frame) isFolded() bool {
	return f.folded && f.IsOpen(RegionInfo)
}

// Solo gives the sidebar the whole window, or hands the other panes their share back, covering
// the panes it takes it from rather than closing them.
func (f *Frame) Solo(on bool) {
	if f.sidebar == "" {
		return
	}

	// Taking the window is the last moment the divider can be read from the sidebar's own side of
	// it, and it costs one call per zoom rather than one per tick.
	if on {
		f.SyncSplit()
	}

	f.solo = on

	if !on {
		defer f.payOwed()
	}

	// This covers or uncovers the pane beside the sidebar, and whatever painted it has long
	// since exited, so the frame renders what stands there again rather than leaving it as it was.
	f.forget(RegionStage)
	f.move(panes.Zoom(f.sidebar, on))
}

// Freeze zooms the sidebar over the window for the pane picker, holding back every move after it
// until Thaw.
func (f *Frame) Freeze() {
	if f.sidebar == "" || f.frozen {
		return
	}

	f.move(panes.Zoom(f.sidebar, true))
	f.send()
	f.frozen = true
}

// Hold holds back every move until Thaw, leaving the window as it stands while a stand-in has the
// sidebar's place.
func (f *Frame) Hold() {
	if f.sidebar == "" {
		return
	}

	f.frozen = true
}

// Thaw sends the moves Freeze held back and puts the zoom back to where Solo left it.
func (f *Frame) Thaw() {
	if !f.frozen {
		return
	}

	held := f.held
	f.frozen, f.held = false, nil

	f.Begin()

	for _, m := range held {
		f.move(m)
	}

	f.move(panes.Zoom(f.sidebar, f.solo))
	f.Flush()
}

// IsFrozen reports whether the pane picker or a stand-in holds the window.
func (f *Frame) IsFrozen() bool {
	return f.frozen
}

// IsSolo reports whether the sidebar is holding the whole window.
func (f *Frame) IsSolo() bool {
	return f.solo
}

// Beside draws the sidebar across part of the stage as well, with the stage beside it, or gives the
// stage those columns back.
func (f *Frame) Beside(on bool) {
	if f.sidebar == "" || f.beside == on {
		return
	}

	f.beside = on

	// Resizing a pane in a zoomed window unzooms it.
	if f.solo {
		f.owed = true

		return
	}

	f.move(panes.Resize(f.sidebar, f.sidebarCols()))
}

// IsBeside reports whether the sidebar is drawn across part of the stage, with the stage beside it.
func (f *Frame) IsBeside() bool {
	return f.beside
}

// FitsBeside reports whether a window of this many columns, with the stage beside the sidebar,
// leaves the stage at least floor columns, the stage being the narrower of the two.
func (f *Frame) FitsBeside(window, floor int) bool {
	return window-f.besideCols(window)-1 >= floor
}

// OpenColumn runs content in a pane split off the columns the sidebar took from the stage, and
// answers the pane, empty where the stage is not beside the sidebar.
func (f *Frame) OpenColumn(content Content) string {
	f.CloseColumn()

	if !f.beside || f.solo {
		return ""
	}

	argv, ok := content.Render(Target{})
	if !ok {
		return ""
	}

	cols := f.sidebarCols() - f.chosenCols() - 1

	f.column = f.tmux.SplitRight(f.sidebar, content.Dir, strconv.Itoa(cols))
	if f.column == "" {
		return ""
	}

	f.beside = false
	f.move(panes.Respawn(f.column, content.Dir, argv...))

	return f.column
}

// CloseColumn takes away the pane OpenColumn split, and reports whether there was one.
func (f *Frame) CloseColumn() bool {
	if f.column == "" {
		return false
	}

	if f.tmux.IsPaneAlive(f.column) {
		f.tmux.KillPane(f.column)
	}

	// tmux hands the closed pane's columns to the sidebar, which leaves the stage beside it.
	f.column, f.beside = "", true

	// Resizing a pane in a zoomed window unzooms it.
	if f.solo {
		f.owed = true

		return true
	}

	// A window resized while the pane was open leaves the sidebar short of the columns it takes
	// beside the stage, and tmux changes nothing for a sidebar already at them.
	f.move(panes.Resize(f.sidebar, f.sidebarCols()))

	return true
}

// SyncKeys puts the frame right about whether the sidebar is still holding the window.
func (f *Frame) SyncKeys() {
	if f.sidebar == "" {
		return
	}

	active, zoomed := f.tmux.GetPaneFocus(f.sidebar)
	f.keys = active

	if f.frozen {
		return
	}

	f.solo = zoomed

	if !zoomed {
		f.payOwed()
	}
}

// Nudge moves the sidebar's edge by a number of columns, wider for a positive one.
func (f *Frame) Nudge(delta int) {
	if f.sidebar == "" {
		return
	}

	total := f.WindowCols()
	if total < 3 {
		return
	}

	current, _ := f.SidebarWidth(total)

	width := min(max(current+delta, 1), total-2)
	if width == current {
		return
	}

	f.cols = width
	f.stageCols = total - width - 1

	// Resizing a pane in a zoomed window unzooms it.
	if f.solo {
		f.owed = true

		return
	}

	f.move(panes.Resize(f.sidebar, f.sidebarCols()))
}

// payOwed puts the split under a zoom that has just been let go at the width chosen during it.
func (f *Frame) payOwed() {
	if !f.owed {
		return
	}

	f.owed = false
	f.move(panes.Resize(f.sidebar, f.sidebarCols()))
}

// HasKeys reports whether the keyboard is on the panel's own pane rather than on a pane beside
// it, as of the last sync.
func (f *Frame) HasKeys() bool {
	return f.keys
}

// sidebarSize is how big tmux has the panel's own pane right now.
func (f *Frame) sidebarSize() (width, height int) {
	if f.sidebar == "" {
		return 0, 0
	}

	return f.tmux.GetPaneWidth(f.sidebar), f.tmux.GetPaneHeight(f.sidebar)
}

// IsStaleSize reports whether tmux has already moved the panel's own pane off the given size.
func (f *Frame) IsStaleSize(width, height int) bool {
	paneWidth, paneHeight := f.sidebarSize()

	return paneWidth > 0 && paneHeight > 0 && (paneWidth != width || paneHeight != height)
}

// SidebarWidth is how many columns the sidebar renders in when it is holding a window of the given
// width, and whether that came from the divider rather than from the config.
func (f *Frame) SidebarWidth(total int) (int, bool) {
	if f.beside && total > 0 {
		return min(f.chosenCols(), total), true
	}

	if width := total - f.stageCols - 1; f.stageCols > 0 && width > 0 {
		return width, true
	}

	if f.cols > 0 && total > 0 {
		return min(f.cols, total), true
	}

	return f.spec.sidebarWidth(total), false
}

// StageCols is how wide the pane beside the sidebar was last measured at.
func (f *Frame) StageCols() int {
	return f.stageCols
}

// WindowCols is how many columns the frame has to lay itself out in.
func (f *Frame) WindowCols() int {
	if f.sidebar == "" {
		return 0
	}

	return f.tmux.GetWindowWidth(f.sidebar)
}

// SyncSplit measures the divider between the sidebar and the pane beside it, so a sidebar rendered
// over the whole window keeps the width the user has dragged the divider to.
func (f *Frame) SyncSplit() {
	if f.owed || f.frozen || f.beside || f.column != "" {
		return
	}

	stage := f.tmux.GetPaneWidth(f.visibleStagePane())
	if stage <= 0 {
		return
	}

	f.stageCols = stage

	width := f.WindowCols() - stage - 1

	// tmux clamping the sidebar into a narrow window leaves the stage at its one-column minimum.
	clamped := stage <= 1 && width < f.cols
	if width > 0 && !clamped {
		f.cols = width
	}
}

// forgetSplit drops the measured divider.
func (f *Frame) forgetSplit() {
	f.stageCols = 0
}

// RestoreSplit gives the sidebar its chosen width again.
func (f *Frame) RestoreSplit() {
	f.forgetSplit()

	if f.sidebar == "" || (!f.hasStage() && !f.isFolded()) {
		return
	}

	if f.solo {
		f.owed = true

		return
	}

	f.tmux.ResizePane(f.restoredSidebar())
}

// IsRemote reports whether the agents are running in an engine this tmux server cannot reach,
// which decides how a terminal is shown and who owns its size.
func (f *Frame) IsRemote() bool {
	return f.remote.IsRemote()
}

// ShellArgv is what a pane runs to open a shell rooted at dir on the engine, empty when the
// engine is this machine.
func (f *Frame) ShellArgv(dir string) []string {
	if len(f.shellCommand) == 0 || strings.TrimSpace(dir) == "" {
		return nil
	}

	return append(slices.Clone(f.shellCommand), "--", dir)
}

// ShowGuest puts an agent's terminal in the stage, by moving its pane in when the engine is
// this machine and by attaching to it over the engine's api when it is not.
func (f *Frame) ShowGuest(guest Guest) {
	defer f.holdSolo()

	f.claimStage()
	if f.stage == "" || guest.Pane == "" {
		return
	}

	if f.remote.IsRemote() {
		f.showRemoteGuest(guest)

		return
	}

	if guest.Pane == f.guest {
		return
	}

	f.restoreGuest()
	f.move(panes.Swap(guest.Pane, f.stage))
	f.guest = guest.Pane
	f.forget(RegionStage)
}

// showRemoteGuest respawns the stage on komrad terminal.
func (f *Frame) showRemoteGuest(guest Guest) {
	if len(f.terminalCommand) == 0 || guest.Agent == "" {
		return
	}

	if f.isShowing(RegionStage, Content{Key: guest.Pane}, Target{Region: RegionStage}) {
		return
	}

	f.move(panes.Respawn(f.stage, "", append(slices.Clone(f.terminalCommand), "--", guest.Agent)...))
	f.shown[RegionStage] = shown{key: guest.Pane}
	f.remoteGuest = guest.Pane
}

// ReleaseGuest returns a guest pane to where it came from before the frame kills it, so the main
// region does not mind a pane that is about to go.
func (f *Frame) ReleaseGuest(pane string) {
	if pane != "" && pane == f.guest {
		f.restoreGuest()
		f.holdSolo()
	}
}

// Vacate returns the guest to its own window and kills the frame's own panes.
func (f *Frame) Vacate() {
	f.abandonStage()
}

// holdSolo says again where the sidebar's zoom belongs, which split-window, swap-pane,
// resize-pane, kill-pane and break-pane all drop silently.
func (f *Frame) holdSolo() {
	// A zoomed sidebar covers the info pane, so the window is left alone while one is up.
	if !f.solo || f.sidebar == "" || f.info != "" {
		return
	}

	f.move(panes.Zoom(f.sidebar, true))
}

func (f *Frame) isShowing(region region, content Content, target Target) bool {
	if content.Key == "" {
		return false
	}

	was := f.shown[region]
	if was.key != content.Key {
		return false
	}

	// Height matters as much as width, with a region giving rows to a split under it and taking
	// them back.
	return !content.Wraps || (was.width == target.Width && was.height == target.Height)
}

// visibleStagePane is the pane the user is actually looking at in the stage slot: the guest
// while one is swapped in, the info pane while the stage is folded into it, and the frame's own
// pane otherwise.
func (f *Frame) visibleStagePane() string {
	if f.guest != "" {
		return f.guest
	}

	if f.folded {
		return f.info
	}

	return f.stage
}

func (f *Frame) restoreGuest() {
	if f.guest == "" {
		return
	}

	switch {
	case !f.tmux.IsPaneAlive(f.guest):
	case f.tmux.IsPaneAlive(f.stage):
		f.move(panes.Swap(f.stage, f.guest))
	default:
		f.tmux.BreakPane(f.guest)
	}

	f.guest = ""
	f.forget(RegionStage)
}

func (f *Frame) reclaim() {
	if f.sidebar == "" {
		return
	}

	for _, pane := range f.tmux.GetSiblingPanes(f.sidebar) {
		if f.tmux.GetPaneOwner(pane) != "" {
			f.tmux.BreakPane(pane)

			continue
		}

		f.tmux.KillPane(pane)
	}
}

func (f *Frame) claimStage() {
	if f.sidebar == "" || f.hasStage() {
		return
	}

	if f.isFolded() {
		f.unfoldStage()

		return
	}

	f.abandonStage()

	f.stage = f.tmux.SplitRight(f.sidebar, getHomeDir(), stageSize)
	f.forget(RegionStage)
	f.forget(RegionInfo)

	if f.stage != "" {
		f.tmux.ResizePane(f.restoredSidebar())
		f.SyncSplit()
	}
}

func (f *Frame) restoredSidebar() (pane, width string) {
	return f.sidebar, strconv.Itoa(f.sidebarCols())
}

// chosenCols is the width the sidebar is put back at, the configured one until anybody moves it.
func (f *Frame) chosenCols() int {
	if f.cols > 0 {
		return f.cols
	}

	return f.spec.sidebarCols
}

// sidebarCols is how wide the sidebar's pane is put, its chosen width plus the columns it takes from
// the stage while the stage is beside it.
func (f *Frame) sidebarCols() int {
	if !f.beside {
		return f.chosenCols()
	}

	return f.besideCols(f.WindowCols())
}

// besideCols is how wide the sidebar's pane is put while the stage is beside it, in a window of
// this many columns.
func (f *Frame) besideCols(window int) int {
	cols := f.chosenCols()

	return cols + 1 + max(window-cols-1, 0)*besidePercent/100
}

// unfoldStage splits the stage back off the info pane it gave its rows to.
func (f *Frame) unfoldStage() {
	split := f.tmux.SplitAbove
	if f.spec.infoAbove {
		split = f.tmux.SplitBelow
	}

	f.stage = split(f.info, getHomeDir(), f.spec.stageOverInfo(f.tmux.GetPaneHeight(f.info)))
	f.folded = false
	f.forget(RegionStage)
}

func (f *Frame) hasStage() bool {
	if f.stage == "" || !f.tmux.IsPaneAlive(f.stage) {
		return false
	}

	return f.guest == "" || f.tmux.IsPaneAlive(f.guest)
}

func (f *Frame) abandonStage() {
	f.CloseColumn()
	f.restoreGuest()

	if f.stage != "" && f.tmux.IsPaneAlive(f.stage) {
		f.tmux.KillPane(f.stage)
	}

	f.closeInfo()
	f.stage, f.folded = "", false
}

func (f *Frame) claimInfo() {
	if f.stage == "" || f.IsOpen(RegionInfo) {
		return
	}

	anchor, home := f.visibleStagePane(), getHomeDir()
	if f.spec.infoAbove {
		f.info = f.tmux.SplitAbove(anchor, home, f.spec.infoSize())
	} else {
		f.info = f.tmux.SplitBelow(anchor, home, f.spec.infoSize())
	}

	f.forget(RegionInfo)
}

func (f *Frame) closeInfo() {
	if f.info == "" {
		return
	}

	f.tmux.KillPane(f.info)
	f.info = ""
	f.forget(RegionInfo)
}

func (f *Frame) forget(region region) {
	delete(f.shown, region)
}

func getHomeDir() string {
	dir, _ := os.UserHomeDir()

	return dir
}
