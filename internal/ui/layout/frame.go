package layout

import (
	"os"

	"weave/config"
)

type Frame struct {
	tmux    Tmux
	spec    spec
	sidebar string
	stage   string
	info    string
	guest   string
	solo    bool

	// moved says the frame has issued a command tmux drops a window's zoom on since the zoom was
	// last put back, which is what separates a call that took the zoom down from the refresh tick
	// asking for content that is already up.
	moved bool
	shown map[Region]shown
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

// Open opens a region and returns the pane it landed in, opening the stage on the way since
// every region hangs off it.
func (f *Frame) Open(region Region) string {
	defer f.restoreSolo()

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

// Close takes a region away, which is only ever the info pane, since the sidebar is the panel
// and the stage is what the rest hangs off.
func (f *Frame) Close(region Region) {
	if region == RegionInfo {
		f.closeInfo()
		f.restoreSolo()
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

	defer f.restoreSolo()

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

	f.run(pane, content.Dir, argv...)
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

	f.solo = on
	f.tmux.SetPaneZoomed(f.sidebar, on)
}

// IsSolo reports whether the sidebar is holding the whole window.
func (f *Frame) IsSolo() bool { return f.solo }

// SyncKeys puts the frame right about whether the sidebar is still holding the window, since tmux
// drops a pane's zoom when the keys move off it and calls into nothing when it does, and a focus
// report cannot tell that from the terminal the whole session is in losing focus, which leaves the
// zoom alone.
func (f *Frame) SyncKeys() {
	if f.sidebar == "" {
		return
	}

	_, zoomed := f.tmux.GetPaneFocus(f.sidebar)
	f.solo = zoomed
}

// SidebarWidth is how many columns the sidebar draws in when it is holding a window of the
// given width, which is what a screen splitting the window itself lays itself out against.
func (f *Frame) SidebarWidth(total int) int { return f.spec.SidebarWidth(total) }

// IsSidebarOnRight is the side the panel's own column sits on, so a screen drawing its own split
// puts it where the panes it took the window from would have.
func (f *Frame) IsSidebarOnRight() bool { return f.spec.sidebarOnRight }

// ShowGuest swaps a pane somebody else owns into the stage.
func (f *Frame) ShowGuest(pane string) {
	defer f.restoreSolo()

	f.claimStage()
	if f.stage == "" || pane == "" || pane == f.guest {
		return
	}

	f.restoreGuest()
	f.tmux.SwapPanes(pane, f.stage)
	f.moved = true
	f.guest = pane
	f.forget(RegionStage)
}

// ReleaseGuest returns a guest pane to where it came from before it is killed, so the main
// region is not left minding a pane that is about to go.
func (f *Frame) ReleaseGuest(pane string) {
	if pane != "" && pane == f.guest {
		f.restoreGuest()
		f.restoreSolo()
	}
}

// restoreSolo puts the sidebar's zoom back after the frame has moved the panes under it, since
// split-window, swap-pane, resize-pane, kill-pane and break-pane each unzoom the window and call
// into nothing when they do.
func (f *Frame) restoreSolo() {
	moved := f.moved
	f.moved = false

	// A zoomed sidebar covers the pane the logs and the editor are drawn in, so the window is
	// left alone while one is up rather than taken back over what was just opened.
	if !moved || !f.solo || f.sidebar == "" || f.info != "" {
		return
	}

	f.tmux.SetPaneZoomed(f.sidebar, true)
}

func (f *Frame) isShowing(region Region, content Content, target Target) bool {
	if content.Key == "" {
		return false
	}

	was := f.shown[region]
	if was.key != content.Key {
		return false
	}

	// Height matters as much as width, since a region gives rows to a split under it and takes
	// them back, and content drawn to a stale height leaves rows the pane never fills.
	return !content.Wraps || (was.width == target.Width && was.height == target.Height)
}

func (f *Frame) getDisplayedPane() string {
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
		f.tmux.SwapPanes(f.stage, f.guest)
		f.moved = true
	}

	f.guest = ""
	f.forget(RegionStage)
}

func (f *Frame) reclaim() {
	if f.sidebar == "" {
		return
	}

	for _, pane := range f.tmux.GetSiblingPanes(f.sidebar) {
		f.moved = true

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
	f.moved = true
	f.forget(RegionStage)
	f.forget(RegionInfo)

	if f.stage != "" {
		f.tmux.ResizePane(f.sidebar, f.spec.sidebarSize)
	}
}

func (f *Frame) splitStage() string {
	if f.spec.sidebarOnRight {
		return f.tmux.SplitLeft(f.sidebar, getHomeDir(), f.spec.stageSize)
	}

	return f.tmux.SplitRight(f.sidebar, getHomeDir(), f.spec.stageSize)
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
		f.moved = true
	}

	f.closeInfo()
	f.stage, f.guest = "", ""
}

func (f *Frame) claimInfo() {
	if f.stage == "" || f.IsOpen(RegionInfo) {
		return
	}

	anchor, home := f.getDisplayedPane(), getHomeDir()
	if f.spec.infoAbove {
		f.info = f.tmux.SplitAbove(anchor, home, f.spec.infoSize)
	} else {
		f.info = f.tmux.SplitBelow(anchor, home, f.spec.infoSize)
	}
	f.moved = true

	f.forget(RegionInfo)
}

func (f *Frame) closeInfo() {
	if f.info == "" {
		return
	}

	f.tmux.KillPane(f.info)
	f.moved = true
	f.info = ""
	f.forget(RegionInfo)
}

func (f *Frame) forget(region Region) { delete(f.shown, region) }

func (f *Frame) run(pane, dir string, argv ...string) {
	f.tmux.RespawnPane(pane, dir, argv...)
}

func getHomeDir() string {
	dir, _ := os.UserHomeDir()

	return dir
}
