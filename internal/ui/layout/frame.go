package layout

import (
	"os"

	"weave/config"
)

type Frame struct {
	tmux Tmux
	spec Spec

	sidebar string
	stage   string
	info    string

	guest string

	shown map[Region]shown
}

type shown struct {
	key   string
	width int
}

// New builds the frame the panel lays out around itself, at the geometry the config asks for.
func New(tmux Tmux, layout config.Layout) *Frame {
	return &Frame{tmux: tmux, spec: NewSpec(layout), shown: map[Region]shown{}}
}

// Attach anchors the frame to the pane the panel runs in and lays the regions out around it.
// That pane is the one the panel was told it runs in rather than one read back from tmux,
// which answers for whatever window the session is currently looking at.
func (f *Frame) Attach(sidebar string) {
	f.sidebar = sidebar
	f.reclaim()
	f.claimStage()
}

// Pane is the pane the frame holds for a region, empty when the region is not open. The main
// region answers with its own pane even while a guest is swapped into it, since a swap
// exchanges where two panes sit rather than taking either away.
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

// Open opens a region and returns the pane it landed in. Every region hangs off the main one,
// so that is opened on the way to any of them.
func (f *Frame) Open(region Region) string {
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

// Close takes a region away. Only the info pane is ever closed, since the sidebar is
// the panel itself and the stage is the frame the rest of it hangs off.
func (f *Frame) Close(region Region) {
	if region == RegionInfo {
		f.closeInfo()
	}
}

// Claimed records that another process has taken a region over, so the next content is drawn
// rather than skipped as already shown.
func (f *Frame) Claimed(region Region) { f.forget(region) }

// Show draws content in a region, opening the region if it is not already there. The sidebar
// is the panel's own pane and is refused, since filling it would replace the process asking
// for it.
func (f *Frame) Show(region Region, content Content) {
	if region != RegionStage && region != RegionInfo {
		return
	}

	f.Open(region)

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
	}

	if f.isShowing(region, content, target.Width) {
		return
	}

	argv, ok := content.Render(target)
	if !ok {
		return
	}

	f.run(pane, argv...)
	f.shown[region] = shown{key: content.Key, width: target.Width}
}

// ShowGuest swaps a pane somebody else owns into the stage.
func (f *Frame) ShowGuest(pane string) {
	f.claimStage()
	if f.stage == "" || pane == "" || pane == f.guest {
		return
	}

	f.restoreGuest()
	f.tmux.SwapPanes(pane, f.stage)
	f.guest = pane
	f.forget(RegionStage)
}

// ReleaseGuest returns a guest pane to where it came from before it is killed, so the main
// region is not left minding a pane that is about to go.
func (f *Frame) ReleaseGuest(pane string) {
	if pane != "" && pane == f.guest {
		f.restoreGuest()
	}
}

func (f *Frame) isShowing(region Region, content Content, width int) bool {
	if content.Key == "" {
		return false
	}

	was := f.shown[region]

	return was.key == content.Key && (!content.Wraps || was.width == width)
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

func (f *Frame) run(pane string, argv ...string) {
	f.tmux.RespawnPane(pane, getHomeDir(), argv...)
}

func getHomeDir() string {
	dir, _ := os.UserHomeDir()

	return dir
}
