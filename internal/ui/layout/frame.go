package layout

import (
	"os"

	"weave/config"
	"weave/internal/panes"
)

type Frame struct {
	tmux    Tmux
	spec    spec
	sidebar string
	stage   string
	info    string
	guest   string
	solo    bool

	// stageCols is how wide the pane beside the sidebar was when the divider was last measured,
	// which is where the sidebar's own width comes from: zooming the sidebar reports it as the
	// whole window while leaving the width of the pane beside it alone.
	stageCols int

	// pending is the pane moves of the change being worked out, sent together by Flush, and
	// sent one at a time while nothing is collecting them.
	pending    []panes.Move
	collecting bool
	shown      map[Region]shown
}

type shown struct {
	key    string
	width  int
	height int
}

// New builds the frame the panel lays out around itself, at the geometry the config asks for.
func New(tmux Tmux, layout config.Layout) *Frame {
	return &Frame{
		tmux:  tmux,
		spec:  newSpec(layout),
		shown: map[Region]shown{},
	}
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
// a guest is swapped into it.
func (f *Frame) Pane(region Region) string {
	switch region {
	case RegionSidebar:
		return f.sidebar
	case RegionStage:
		return f.stage
	case RegionInfo:
		return f.info
	case RegionNone:
	}

	return ""
}

// GuestPane is the pane somebody else owns currently swapped into the stage.
func (f *Frame) GuestPane() string { return f.guest }

// Begin collects the frame's pane moves for Flush to send together, so a change that moves
// several panes is one invocation of tmux and so one repaint rather than one for each.
func (f *Frame) Begin() { f.collecting = true }

// Flush sends the pane moves collected since Begin, and does nothing when there were none.
func (f *Frame) Flush() {
	moves := f.pending
	f.pending, f.collecting = nil, false

	if len(moves) > 0 {
		f.tmux.MovePanes(moves)
	}
}

// IsCollecting reports whether a change is still being collected, which is what says the panes
// have not moved yet, so reading anything back off them would answer for the layout as it stood.
func (f *Frame) IsCollecting() bool { return f.collecting }

// move records a pane move, sending it at once unless a change is being collected around it.
func (f *Frame) move(m panes.Move) {
	f.pending = append(f.pending, m)

	if !f.collecting {
		f.Flush()
	}
}

// Open opens a region and returns the pane it landed in, opening the stage on the way since
// every region hangs off it.
func (f *Frame) Open(region Region) string {
	defer f.holdSolo()

	return f.open(region)
}

func (f *Frame) open(region Region) string {
	switch region {
	case RegionStage:
		f.claimStage()
	case RegionInfo:
		f.claimStage()
		f.claimInfo()
	case RegionSidebar, RegionNone:
	}

	return f.Pane(region)
}

// IsOpen reports whether a region's pane is still running, which is how a pane the user closed
// themselves is noticed.
func (f *Frame) IsOpen(region Region) bool {
	pane := f.Pane(region)

	return pane != "" && f.tmux.IsPaneAlive(pane)
}

// Close takes a region away, which is only ever the info pane.
func (f *Frame) Close(region Region) {
	if region == RegionInfo {
		f.closeInfo()
		f.holdSolo()
	}
}

// Claimed records that another process has taken a region over, so the next content is drawn
// rather than skipped as already shown.
func (f *Frame) Claimed(region Region) { f.forget(region) }

// Show draws content in a region, opening it first when needed, and refuses the sidebar since
// filling it would replace the panel itself.
func (f *Frame) Show(region Region, content Content) {
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

	f.move(panes.Respawn(pane, content.Dir, argv...))
	f.shown[region] = shown{
		key:    content.Key,
		width:  target.Width,
		height: target.Height,
	}
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
	f.move(panes.Zoom(f.sidebar, on))
}

// IsSolo reports whether the sidebar is holding the whole window.
func (f *Frame) IsSolo() bool { return f.solo }

// SyncKeys puts the frame right about whether the sidebar is still holding the window, which tmux
// drops when the keys move off it and reports nothing about.
func (f *Frame) SyncKeys() {
	if f.sidebar == "" {
		return
	}

	_, zoomed := f.tmux.GetPaneFocus(f.sidebar)
	f.solo = zoomed
}

// SidebarSize is how big tmux has the panel's own pane right now.
func (f *Frame) SidebarSize() (width, height int) {
	if f.sidebar == "" {
		return 0, 0
	}

	return f.tmux.GetPaneWidth(f.sidebar), f.tmux.GetPaneHeight(f.sidebar)
}

// SidebarWidth is how many columns the sidebar draws in when it is holding a window of the given
// width, and whether that came from the divider rather than from the config.
func (f *Frame) SidebarWidth(total int) (int, bool) {
	if width := total - f.stageCols - 1; f.stageCols > 0 && width > 0 {
		return width, true
	}

	return f.spec.SidebarWidth(total), false
}

// StageCols is how wide the pane beside the sidebar was last measured at, which is the width
// anything shown there is given.
func (f *Frame) StageCols() int { return f.stageCols }

// SyncSplit measures the divider between the sidebar and the pane beside it, so a sidebar drawn
// over the whole window keeps the width the user has dragged the divider to.
func (f *Frame) SyncSplit() {
	f.stageCols = f.tmux.GetPaneWidth(f.visibleStagePane())
}

// IsSidebarOnRight is the side the panel's own column sits on, so a screen drawing its own split
// puts it where the panes it took the window from would have.
func (f *Frame) IsSidebarOnRight() bool { return f.spec.sidebarOnRight }

// ShowGuest swaps a pane somebody else owns into the stage.
func (f *Frame) ShowGuest(pane string) {
	defer f.holdSolo()

	f.claimStage()
	if f.stage == "" || pane == "" || pane == f.guest {
		return
	}

	f.restoreGuest()
	f.move(panes.Swap(pane, f.stage))
	f.guest = pane
	f.forget(RegionStage)
}

// ReleaseGuest returns a guest pane to where it came from before it is killed, so the main
// region is not left minding a pane that is about to go.
func (f *Frame) ReleaseGuest(pane string) {
	if pane != "" && pane == f.guest {
		f.restoreGuest()
		f.holdSolo()
	}
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

func (f *Frame) isShowing(region Region, content Content, target Target) bool {
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

// visibleStagePane is the pane the user is actually looking at in the stage slot, which is the
// guest while one is swapped in and the frame's own pane otherwise.
func (f *Frame) visibleStagePane() string {
	if f.guest != "" {
		return f.guest
	}

	return f.stage
}

func (f *Frame) restoreGuest() {
	if f.guest == "" {
		return
	}

	if f.tmux.IsPaneAlive(f.guest) {
		f.move(panes.Swap(f.stage, f.guest))
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

	f.abandonStage()

	f.stage = f.splitStage()
	f.forget(RegionStage)
	f.forget(RegionInfo)

	if f.stage != "" {
		f.tmux.ResizePane(f.sidebar, f.spec.sidebarSize)
		f.SyncSplit()
	}
}

func (f *Frame) splitStage() string {
	if f.spec.sidebarOnRight {
		return f.tmux.SplitLeft(f.sidebar, getHomeDir(), stageSize)
	}

	return f.tmux.SplitRight(f.sidebar, getHomeDir(), stageSize)
}

func (f *Frame) hasStage() bool {
	if f.stage == "" || !f.tmux.IsPaneAlive(f.stage) {
		return false
	}

	return f.guest == "" || f.tmux.IsPaneAlive(f.guest)
}

func (f *Frame) abandonStage() {
	if f.stage != "" && f.tmux.IsPaneAlive(f.stage) {
		f.tmux.KillPane(f.stage)
	}

	f.closeInfo()
	f.stage, f.guest = "", ""
}

func (f *Frame) claimInfo() {
	if f.stage == "" || f.IsOpen(RegionInfo) {
		return
	}

	anchor, home := f.visibleStagePane(), getHomeDir()
	if f.spec.infoAbove {
		f.info = f.tmux.SplitAbove(anchor, home, f.spec.infoSize)
	} else {
		f.info = f.tmux.SplitBelow(anchor, home, f.spec.infoSize)
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

func (f *Frame) forget(region Region) { delete(f.shown, region) }

func getHomeDir() string {
	dir, _ := os.UserHomeDir()

	return dir
}
