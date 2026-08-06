package layout

import (
	"os"

	"weave/config"
)

// Frame is the pane each region occupies and what was last drawn in it. It drives tmux
// directly rather than asking the service, since a keypress that moves the cursor moves the
// frame with it and cannot pay a round trip to do it.
type Frame struct {
	tmux Tmux
	spec Spec

	nav    string
	main   string
	bottom string
	// guest is a pane somebody else owns that has been swapped into the main region, an
	// agent's own pane being the one thing the panel shows without owning it.
	guest string

	// shown is what each region was last given, which content is compared against so a
	// redraw does not restart a pager somebody is reading.
	shown map[Region]shown
}

// shown is the key of the content written to a region and the width it was rendered for.
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
func (f *Frame) Attach(nav string) {
	f.nav = nav
	f.reclaim()
	f.Apply()
}

// Apply brings the frame to its spec, opening the main region and sizing it. It is idempotent,
// so it is safe to run again whenever the frame may have been moved out from under the panel.
func (f *Frame) Apply() {
	f.claimMain()

	if f.main != "" {
		f.tmux.ResizePane(f.main, f.spec.getMainPercent())
	}
}

// Pane is the pane the frame holds for a region, empty when the region is not open. The main
// region answers with its own pane even while a guest is swapped into it, since a swap
// exchanges where two panes sit rather than taking either away.
func (f *Frame) Pane(region Region) string {
	switch region {
	case RegionNav:
		return f.nav
	case RegionMain:
		return f.main
	case RegionBottom:
		return f.bottom
	case RegionNone:
	}

	return ""
}

// GuestPane is the pane somebody else owns currently swapped into the main region.
func (f *Frame) GuestPane() string { return f.guest }

// Open opens a region and returns the pane it landed in. Every region hangs off the main one,
// so that is opened on the way to any of them.
func (f *Frame) Open(region Region) string {
	switch region {
	case RegionMain:
		f.claimMain()
	case RegionBottom:
		f.claimMain()
		f.claimBottom()
	case RegionNav, RegionNone:
	}

	return f.Pane(region)
}

// IsOpen reports whether a region's pane is still running, which is how a pane the user closed
// themselves is noticed.
func (f *Frame) IsOpen(region Region) bool {
	pane := f.Pane(region)

	return pane != "" && f.tmux.IsPaneAlive(pane)
}

// Close takes a region away. Only the bottom region is ever closed, since the nav region is
// the panel itself and the main region is the frame the rest of it hangs off.
func (f *Frame) Close(region Region) {
	if region == RegionBottom {
		f.closeBottom()
	}
}

// Claimed records that another process has taken a region over, so the next content is drawn
// rather than skipped as already shown.
func (f *Frame) Claimed(region Region) { f.forget(region) }

// Show draws content in a region, opening the region if it is not already there. The nav
// region is the panel's own pane and is refused, since filling it would replace the process
// asking for it.
func (f *Frame) Show(region Region, content Content) {
	if region != RegionMain && region != RegionBottom {
		return
	}

	f.Open(region)

	// The main region cannot hold a guest and a document at once.
	if region == RegionMain {
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

// ShowGuest swaps a pane somebody else owns into the main region.
func (f *Frame) ShowGuest(pane string) {
	f.claimMain()
	if f.main == "" || pane == "" || pane == f.guest {
		return
	}

	f.restoreGuest()
	f.tmux.SwapPanes(pane, f.main)
	f.guest = pane
	f.forget(RegionMain)
}

// ReleaseGuest returns a guest pane to where it came from before it is killed, so the main
// region is not left minding a pane that is about to go.
func (f *Frame) ReleaseGuest(pane string) {
	if pane != "" && pane == f.guest {
		f.restoreGuest()
	}
}

// isShowing reports whether a region already holds this content, laid out to the width it
// would be drawn at now.
func (f *Frame) isShowing(region Region, content Content, width int) bool {
	if content.Key == "" {
		return false
	}

	was := f.shown[region]

	return was.key == content.Key && (!content.Wraps || was.width == width)
}

// getDisplayedPane is the pane on screen in the main region, which is the guest's while one is
// swapped in.
func (f *Frame) getDisplayedPane() string {
	if f.guest != "" {
		return f.guest
	}

	return f.main
}

// restoreGuest puts the guest pane back where it came from, leaving the main region holding
// nothing the frame knows about since the swap took what it drew there off screen.
func (f *Frame) restoreGuest() {
	if f.guest == "" {
		return
	}

	if f.tmux.IsPaneAlive(f.guest) {
		f.tmux.SwapPanes(f.main, f.guest)
	}

	f.guest = ""
	f.forget(RegionMain)
}

// reclaim clears the panes a previous panel left in the window. A pane an agent owns is broken
// out into a window of its own rather than killed, since the agent in it is still running.
func (f *Frame) reclaim() {
	if f.nav == "" {
		return
	}

	for _, pane := range f.tmux.GetSiblingPanes(f.nav) {
		if f.tmux.GetPaneOwner(pane) != "" {
			f.tmux.BreakPane(pane)

			continue
		}

		f.tmux.KillPane(pane)
	}
}

// claimMain opens the main region, rebuilding it when it or the guest pane it was minding has
// died. Whatever was drawn in it, and in the region under it, goes with it.
func (f *Frame) claimMain() {
	if f.nav == "" || f.hasMain() {
		return
	}

	f.abandonMain()

	f.main = f.tmux.SplitRight(f.nav, getHomeDir(), f.spec.getMainPercent())
	f.forget(RegionMain)
	f.forget(RegionBottom)
}

func (f *Frame) hasMain() bool {
	if f.main == "" || !f.tmux.IsPaneAlive(f.main) {
		return false
	}

	return f.guest == "" || f.tmux.IsPaneAlive(f.guest)
}

func (f *Frame) abandonMain() {
	if f.main != "" && f.tmux.IsPaneAlive(f.main) {
		f.tmux.KillPane(f.main)
	}

	f.closeBottom()
	f.main, f.guest = "", ""
}

// claimBottom opens the bottom region under whatever the main one is showing, a guest pane
// included, so the two move together.
func (f *Frame) claimBottom() {
	if f.main == "" || f.IsOpen(RegionBottom) {
		return
	}

	f.bottom = f.tmux.SplitBelow(f.getDisplayedPane(), getHomeDir(), f.spec.getBottomPercent())
	f.forget(RegionBottom)
}

func (f *Frame) closeBottom() {
	if f.bottom == "" {
		return
	}

	f.tmux.KillPane(f.bottom)
	f.bottom = ""
	f.forget(RegionBottom)
}

func (f *Frame) forget(region Region) { delete(f.shown, region) }

// run replaces whatever a pane is showing with a new command.
func (f *Frame) run(pane string, argv ...string) {
	f.tmux.RespawnPane(pane, getHomeDir(), argv...)
}

func getHomeDir() string {
	dir, _ := os.UserHomeDir()

	return dir
}
