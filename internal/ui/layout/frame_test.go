package layout

import (
	"strings"
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFrameAttachAnchorsToTheGivenPane covers which pane the frame is laid out around. It is
// the one the panel was told it runs in, never one read from tmux, which answers for
// whatever window the session is currently looking at. A panel restarted while the session
// sits on an agent would otherwise split its stage into that agent's window.
func TestFrameAttachAnchorsToTheGivenPane(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())

	frame.Attach("%7")

	assert.Equal(t, []string{"%7"}, tmux.anchors)
	assert.Equal(t, "%7", frame.Pane(RegionSidebar))
}

// TestFrameApplySizesTheMainRegion covers a stage pane that outlived the panel. It is not
// split again, so the resize is the only thing that puts it at the configured split.
func TestFrameApplySizesTheMainRegion(t *testing.T) {
	tmux := &recordingTmux{}
	New(tmux, config.Layout{SidebarWidth: 0.4}).Attach("%0")

	assert.Equal(t, []string{"%0 40%"}, tmux.resizes, "the sidebar is what is put at the configured width")
}

// TestFrameApplyIsIdempotent covers running the reconcile again over a frame already laid
// out, which is what lets anything that suspects the frame has moved just ask for it to be
// squared rather than working out whether it needs to be.
func TestFrameApplyIsIdempotent(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	frame.Apply()
	frame.Apply()

	assert.Len(t, tmux.anchors, 1, "the stage is split once")
	assert.Empty(t, tmux.killed, "and nothing already standing is torn down")
}

// TestFrameLaysRegionsOutOnTheConfiguredSides covers a layout the user has turned around. The
// stage goes on whichever side of the panel is left for it, and the info pane opens against
// whichever side of the stage the config names.
func TestFrameLaysRegionsOutOnTheConfiguredSides(t *testing.T) {
	tests := []struct {
		name   string
		layout config.Layout
		want   []string
	}{
		{
			name:   "the defaults put the stage right of the sidebar and the info pane below it",
			layout: config.Layout{},
			want:   []string{"right %0 80%", "below %1 40%"},
		},
		{
			name:   "a sidebar on the right leaves the stage the left",
			layout: config.Layout{SidebarSide: config.SidebarSideRight},
			want:   []string{"left %0 80%", "below %1 40%"},
		},
		{
			name:   "an info pane above opens over the stage",
			layout: config.Layout{InfoSide: config.InfoSideAbove},
			want:   []string{"right %0 80%", "above %1 40%"},
		},
		{
			name:   "a width in columns splits the stage evenly and resizes afterwards",
			layout: config.Layout{SidebarWidth: 40},
			want:   []string{"right %0 50%", "below %1 40%"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{}
			frame := New(tmux, tt.layout)

			frame.Attach("%0")
			frame.Open(RegionInfo)

			assert.Equal(t, tt.want, tmux.splits)
		})
	}
}

// TestFrameApplySizesTheNavPaneInColumns covers a width the config states in columns, which
// tmux cannot be asked for as the remainder of a split and so is set on the pane itself.
func TestFrameApplySizesTheNavPaneInColumns(t *testing.T) {
	tmux := &recordingTmux{}

	New(tmux, config.Layout{SidebarWidth: 40}).Attach("%0")

	assert.Equal(t, []string{"%0 40"}, tmux.resizes)
}

// TestFrameShowMeasuresThePane covers the width content is laid out to, which is the pane it
// lands in as tmux reports it and not the share of the window the spec asked for.
func TestFrameShowMeasuresThePane(t *testing.T) {
	tmux := &recordingTmux{width: 216}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	var got Target
	frame.Show(RegionStage, newProbe("doc", true, &got))

	assert.Equal(t, Target{Region: RegionStage, Width: 216}, got)
}

// TestFrameShowSkipsTheWidthOfContentThatIgnoresIt covers a log or anything else that lays
// itself out. The pane is not measured for it, since the answer would go nowhere, and it is
// not redrawn when the pane changes width.
func TestFrameShowSkipsTheWidthOfContentThatIgnoresIt(t *testing.T) {
	tmux := &recordingTmux{width: 216}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	var got Target
	frame.Show(RegionStage, newProbe("log", false, &got))
	assert.Equal(t, Target{Region: RegionStage}, got)

	tmux.width = 100
	frame.Show(RegionStage, newProbe("log", false, &got))

	assert.Len(t, tmux.respawns, 1, "a narrower pane is still showing the same thing")
}

// TestFrameShowRedrawsContentThatWrapped covers a pane that has changed width under content
// laid out to the width it used to be. The document has not changed, but what is on screen
// is wrapped to a pane that is no longer there.
func TestFrameShowRedrawsContentThatWrapped(t *testing.T) {
	tmux := &recordingTmux{width: 216}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	var got Target
	frame.Show(RegionStage, newProbe("doc", true, &got))
	frame.Show(RegionStage, newProbe("doc", true, &got))
	require.Len(t, tmux.respawns, 1, "the same document at the same width draws once")

	tmux.width = 100
	frame.Show(RegionStage, newProbe("doc", true, &got))

	assert.Len(t, tmux.respawns, 2)
	assert.Equal(t, 100, got.Width, "and it is laid out to the pane as it is now")
}

// TestFrameShowLeavesThePagerAlone covers the pass that redraws the frame every second.
// Content is compared against what its region is already showing, so a document left up is
// not respawned under somebody reading it.
func TestFrameShowLeavesThePagerAlone(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	frame.Show(RegionInfo, newDocument("# auth"))
	frame.Show(RegionInfo, newDocument("# auth"))

	assert.Len(t, tmux.respawns, 1)
	assert.Equal(t, []string{"%2"}, tmux.respawnPanes, "the record goes below, never into the stage")
}

// TestFrameShowKeylessContentAlwaysDraws covers content that names nothing it could be
// compared against, which is how a flash asks to be drawn over what it is flashing and
// leaves the region holding nothing the plain document after it matches.
func TestFrameShowKeylessContentAlwaysDraws(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	frame.Show(RegionStage, newDocument("# auth"))
	frame.Show(RegionStage, Content{Render: func(Target) ([]string, bool) { return []string{"flash"}, true }})
	frame.Show(RegionStage, newDocument("# auth"))

	assert.Len(t, tmux.respawns, 3)
}

// TestFrameShowLeavesAPaneItCouldNotRenderFor covers content that could not be prepared,
// most often a temporary file that would not write. The pane keeps what it had rather than
// being blanked, and it is not remembered as showing what never reached it.
func TestFrameShowLeavesAPaneItCouldNotRenderFor(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	frame.Show(RegionStage, newDocument("# auth"))
	frame.Show(RegionStage, Content{Key: "text:# db", Render: func(Target) ([]string, bool) { return nil, false }})

	require.Len(t, tmux.respawns, 1, "the pane was left as it was")

	frame.Show(RegionStage, newDocument("# db"))

	assert.Len(t, tmux.respawns, 2, "and the document that failed is not held as shown")
}

// TestFrameShowRefusesTheNavRegion covers the one pane the frame will not fill. The panel
// draws its own, and filling it would replace the process that asked.
func TestFrameShowRefusesTheNavRegion(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	frame.Show(RegionSidebar, newDocument("# auth"))

	assert.Empty(t, tmux.respawns)
}

// TestFrameBottomRegion covers the info pane. It is split beneath whatever fills
// the stage, opened once however many times it is asked for, and taken away whole.
func TestFrameBottomRegion(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")
	frame.ShowGuest("%9")

	pane := frame.Open(RegionInfo)

	require.Equal(t, "%2", pane)
	assert.Equal(t, []string{"%9"}, tmux.belowAnchors, "it hangs off the pane the stage holds")
	assert.True(t, frame.IsOpen(RegionInfo))

	assert.Equal(t, pane, frame.Open(RegionInfo), "a second ask is the pane already open")
	assert.Len(t, tmux.belowAnchors, 1, "and splits nothing")

	frame.Close(RegionInfo)

	assert.Contains(t, tmux.killed, pane)
	assert.False(t, frame.IsOpen(RegionInfo))
}

// TestFrameBottomGoesWithARebuiltMain covers the stage losing its pane with the one
// below still open. That pane hangs off the stage's own, so it cannot outlive it in a
// window nothing will find again.
func TestFrameBottomGoesWithARebuiltMain(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")
	require.NotEmpty(t, frame.Open(RegionInfo))

	tmux.deadPanes = map[string]bool{frame.Pane(RegionStage): true}
	frame.Show(RegionStage, newDocument("# rebuilt"))

	assert.False(t, frame.IsOpen(RegionInfo))
	assert.Contains(t, tmux.killed, "%2")
}

// TestFrameGuestHoldsTheMainRegion covers an agent's own pane swapped into the stage. It is
// the pane the stage is showing while it is there, and the document that displaces it
// puts it back.
func TestFrameGuestHoldsTheMainRegion(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	frame.ShowGuest("%9")

	assert.Equal(t, "%9", frame.GuestPane())
	assert.Equal(t, "%1", frame.Pane(RegionStage), "the region keeps its own pane, away in the agent's window")
	require.Equal(t, 1, tmux.swaps)

	frame.ShowGuest("%9")
	assert.Equal(t, 1, tmux.swaps, "the agent already staged is not swapped again")

	frame.Show(RegionStage, newDocument("# auth"))

	assert.Empty(t, frame.GuestPane(), "a document sends the agent back")
	assert.Equal(t, 2, tmux.swaps)
	assert.Equal(t, []string{"%1"}, tmux.respawnPanes, "and is drawn in the region's own pane")
}

// TestFrameGuestDisplacesWhatWasDrawn covers a document that was on the stage before an
// agent was swapped in. The swap took it off screen, so hovering back to it draws it again
// rather than reading as the pane already having it.
func TestFrameGuestDisplacesWhatWasDrawn(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	frame.Show(RegionStage, newDocument("# auth"))
	frame.ShowGuest("%9")
	frame.Show(RegionStage, newDocument("# auth"))

	assert.Len(t, tmux.respawns, 2)
}

// TestFrameReleaseGuest covers an agent about to be killed. Its pane leaves the stage first,
// so the region is not left minding a pane that is going away.
func TestFrameReleaseGuest(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")
	frame.ShowGuest("%9")

	frame.ReleaseGuest("%8")
	assert.Equal(t, "%9", frame.GuestPane(), "another agent's pane is not the one staged")

	frame.ReleaseGuest("%9")
	assert.Empty(t, frame.GuestPane())
}

// TestFrameClaimedForgetsWhatWasDrawn covers a region another process has taken over, which
// is how an action that fills the pane itself says the frame no longer knows what is in it.
func TestFrameClaimedForgetsWhatWasDrawn(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	frame.Show(RegionInfo, newDocument("# auth"))
	frame.Claimed(RegionInfo)
	frame.Show(RegionInfo, newDocument("# auth"))

	assert.Len(t, tmux.respawns, 2)
}

// TestFrameAttachReclaimsTheWindow covers the panel starting into a window the last one left
// panes in, which is what a reload or a panel that died leaves behind. An agent staged at the
// time is sitting in that window as its own pane, so it goes back to a window of its own, and
// the last frame's pane is killed rather than left for the new one to split beside.
func TestFrameAttachReclaimsTheWindow(t *testing.T) {
	tmux := &recordingTmux{
		siblings: []string{"%7", "%8"},
		owners:   map[string]string{"%7": "019fbb22-3735-71a9-ad83-acf4aa537acb"},
	}

	New(tmux, config.Layout{SidebarWidth: 0.4}).Attach("%0")

	assert.Equal(t, []string{"%7"}, tmux.broken, "a staged agent is returned to its own window")
	assert.Equal(t, []string{"%8"}, tmux.killed, "the last frame's own pane is not kept")
}

// TestFrameAttachLeavesAnEmptyWindowAlone covers the ordinary first start, where the panel is
// the only pane. Nothing is reclaimed, since there is nothing there to reclaim.
func TestFrameAttachLeavesAnEmptyWindowAlone(t *testing.T) {
	tmux := &recordingTmux{}

	New(tmux, config.Layout{SidebarWidth: 0.4}).Attach("%0")

	assert.Empty(t, tmux.broken)
	assert.Empty(t, tmux.killed)
}

// newDocument is content standing in for a rendered markdown document, identified by what it
// says and laid out to the pane it lands in.
func newDocument(markdown string) Content {
	return Content{
		Key:    "text:" + markdown,
		Wraps:  true,
		Render: func(Target) ([]string, bool) { return []string{"less", markdown}, true },
	}
}

// newProbe is content that records the target it was rendered for, so a test can assert what
// the frame told it about the pane.
func newProbe(key string, wraps bool, got *Target) Content {
	return Content{
		Key:   key,
		Wraps: wraps,
		Render: func(target Target) ([]string, bool) {
			*got = target

			return []string{"less", key}, true
		},
	}
}

// recordingTmux records what the frame does to a window rather than laying panes out. Panes are
// handed out in the order tmux would, so the stage is %1 and the one under it %2.
type recordingTmux struct {
	// width is what GetPaneWidth reports for every pane, the columns content lays out to.
	width int
	// splits are the panes each region has been split off, each recorded as the direction it
	// went followed by the anchor and the size tmux was given.
	splits []string
	// anchors are the panes the stage has been split beside, which is the pane the
	// frame believes the panel runs in.
	anchors []string
	// belowAnchors are the panes the info pane has been split against.
	belowAnchors []string
	// resizes are the widths panes have been set to, each the pane followed by the size.
	resizes []string
	swaps   int
	// respawns are the argv each pane has been filled with, alongside the panes themselves.
	respawns     []string
	respawnPanes []string
	// siblings are the panes GetSiblingPanes reports beside the panel's own, and owners says
	// which of them carry an agent's stamp. Together they are the window a replaced panel
	// left behind.
	siblings []string
	owners   map[string]string
	// broken and killed are what reclaim did with those panes.
	broken []string
	killed []string
	// deadPanes are the panes IsPaneAlive answers no for, which is how a test stands a pane
	// up and then takes it away.
	deadPanes map[string]bool
}

func (t *recordingTmux) IsPaneAlive(pane string) bool { return !t.deadPanes[pane] }
func (t *recordingTmux) GetPaneWidth(string) int      { return t.width }

func (t *recordingTmux) SplitRight(anchor, _, size string) string {
	return t.recordSplit("right", anchor, size)
}

func (t *recordingTmux) SplitLeft(anchor, _, size string) string {
	return t.recordSplit("left", anchor, size)
}

func (t *recordingTmux) SplitBelow(anchor, _, size string) string {
	return t.recordSplit("below", anchor, size)
}

func (t *recordingTmux) SplitAbove(anchor, _, size string) string {
	return t.recordSplit("above", anchor, size)
}

// recordSplit notes a split and hands back the pane tmux would, the stage when the split was
// beside the anchor and the info pane under it when the split was above or below.
func (t *recordingTmux) recordSplit(direction, anchor, size string) string {
	t.splits = append(t.splits, strings.Join([]string{direction, anchor, size}, " "))

	if direction == "left" || direction == "right" {
		t.anchors = append(t.anchors, anchor)

		return "%1"
	}

	t.belowAnchors = append(t.belowAnchors, anchor)

	return "%2"
}

func (t *recordingTmux) ResizePane(pane, width string) {
	t.resizes = append(t.resizes, pane+" "+width)
}

func (t *recordingTmux) SwapPanes(string, string) { t.swaps++ }

func (t *recordingTmux) RespawnPane(pane, _ string, argv ...string) {
	t.respawns = append(t.respawns, strings.Join(argv, " "))
	t.respawnPanes = append(t.respawnPanes, pane)
}

func (t *recordingTmux) GetSiblingPanes(string) []string { return t.siblings }
func (t *recordingTmux) GetPaneOwner(pane string) string { return t.owners[pane] }
func (t *recordingTmux) BreakPane(pane string)           { t.broken = append(t.broken, pane) }
func (t *recordingTmux) KillPane(pane string)            { t.killed = append(t.killed, pane) }
