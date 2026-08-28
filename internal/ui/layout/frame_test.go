package layout

import (
	"strings"
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFrameAttachAnchorsToTheGivenPane covers a panel restarted while the session sits on an
// agent, which would otherwise split its stage into that agent's window.
func TestFrameAttachAnchorsToTheGivenPane(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())

	frame.Attach("%7")

	assert.Equal(t, []string{"%7"}, tmux.anchors)
	assert.Equal(t, "%7", frame.Pane(RegionSidebar))
}

// TestFrameSizesTheNavPane covers which of the two panes a split leaves is put at the width
// the config states, which is the sidebar, since that is the one the width is stated for.
func TestFrameSizesTheNavPane(t *testing.T) {
	tmux := &recordingTmux{}
	New(tmux, config.Layout{SidebarWidth: 0.4}).Attach("%0")

	assert.Equal(t, []string{"%0 40%"}, tmux.resizes, "the sidebar is what is put at the configured width")
}

// TestFrameRedrawsWhenOnlyTheHeightMoves covers the stage keeping its width while a split opens
// under it, which leaves rows blank if the height is never re-read.
func TestFrameRedrawsWhenOnlyTheHeightMoves(t *testing.T) {
	tmux := &recordingTmux{width: 80, height: 40}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	var heights []int
	content := Content{
		Key:   "diff",
		Wraps: true,
		Render: func(target Target) ([]string, bool) {
			heights = append(heights, target.Height)

			return []string{"draw"}, true
		},
	}

	frame.Show(RegionStage, content)
	frame.Show(RegionStage, content)
	tmux.height = 24
	frame.Show(RegionStage, content)
	tmux.height = 40
	frame.Show(RegionStage, content)

	assert.Equal(t, []int{40, 24, 40}, heights,
		"the same content at an unchanged size is left alone, and every height change is redrawn")
}

// TestFrameOpenLeavesAStandingFrameAlone covers asking for a region that is already there,
// which is what lets anything that suspects the frame has moved just ask for it rather than
// working out whether it needs to.
func TestFrameOpenLeavesAStandingFrameAlone(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	frame.Open(RegionStage)
	frame.Open(RegionStage)

	assert.Len(t, tmux.anchors, 1, "the stage is split once")
	assert.Len(t, tmux.resizes, 1, "and the sidebar is not resized under a frame already at its width")
	assert.Empty(t, tmux.killed, "and nothing already standing is torn down")
}

// TestFrameLaysRegionsOutOnTheConfiguredSides covers a layout the user has turned around, where
// the stage and the info pane both follow the config.
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

// TestFrameSizesTheNavPaneInColumns covers a width the config states in columns, which
// tmux cannot be asked for as the remainder of a split and so is set on the pane itself.
func TestFrameSizesTheNavPaneInColumns(t *testing.T) {
	tmux := &recordingTmux{}

	New(tmux, config.Layout{SidebarWidth: 40}).Attach("%0")

	assert.Equal(t, []string{"%0 40"}, tmux.resizes)
}

// TestFrameSizesTheNavPaneInColumnsAgainAfterARebuild covers the stage losing its pane, since a
// rebuilt stage is split evenly unless it is sized the way the first one was.
func TestFrameSizesTheNavPaneInColumnsAgainAfterARebuild(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.Layout{SidebarWidth: 40})
	frame.Attach("%0")

	tmux.deadPanes = map[string]bool{frame.Pane(RegionStage): true}
	frame.Show(RegionStage, newDocument("# rebuilt"))

	require.Len(t, tmux.splits, 2, "the dead stage was split again")
	assert.Equal(t, []string{"%0 40", "%0 40"}, tmux.resizes)
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

// TestFrameShowSkipsTheWidthOfContentThatIgnoresIt covers a log that lays itself out, which is
// neither measured nor redrawn when the pane changes width.
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
// still wrapped to the width it used to be.
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

// TestFrameShowLeavesThePagerAlone covers the pass that redraws the frame every second, which
// must not respawn a document under somebody reading it.
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

// TestFrameShowLeavesAPaneItCouldNotRenderFor covers content that would not prepare, where the
// pane keeps what it had rather than being blanked.
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

// TestFrameShowRefusesTheNavRegion covers the one pane the frame will not fill, since filling
// it would replace the process that asked.
func TestFrameShowRefusesTheNavRegion(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	frame.Show(RegionSidebar, newDocument("# auth"))

	assert.Empty(t, tmux.respawns)
}

// TestFrameBottomRegion covers the info pane, which opens once however many times it is asked
// for and is taken away whole.
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

// TestFrameBottomGoesWithARebuiltMain covers the stage losing its pane with the one below still
// open, which would otherwise be left in a window nothing will find again.
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

// TestFrameGuestHoldsTheMainRegion covers an agent's own pane swapped into the stage, which the
// stage shows until a document displaces it.
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

// TestFrameGuestDisplacesWhatWasDrawn covers a document that was on the stage before an agent
// was swapped in, which has to be drawn again rather than read as still there.
func TestFrameGuestDisplacesWhatWasDrawn(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")

	frame.Show(RegionStage, newDocument("# auth"))
	frame.ShowGuest("%9")
	frame.Show(RegionStage, newDocument("# auth"))

	assert.Len(t, tmux.respawns, 2)
}

// TestFrameReleaseGuest covers an agent about to be killed, whose pane leaves the stage first
// so the region is not left minding it.
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
// panes in, where a staged agent goes back to its own window and the dead pane is killed.
func TestFrameAttachReclaimsTheWindow(t *testing.T) {
	tmux := &recordingTmux{
		siblings: []string{"%7", "%8"},
		owners:   map[string]string{"%7": "019fbb22-3735-71a9-ad83-acf4aa537acb"},
	}

	New(tmux, config.Layout{SidebarWidth: 0.4}).Attach("%0")

	assert.Equal(t, []string{"%7"}, tmux.broken, "a staged agent is returned to its own window")
	assert.Equal(t, []string{"%8"}, tmux.killed, "the last frame's own pane is not kept")
}

// TestFrameAttachLeavesAnEmptyWindowAlone covers the ordinary first start, where there is
// nothing to reclaim.
func TestFrameAttachLeavesAnEmptyWindowAlone(t *testing.T) {
	tmux := &recordingTmux{}

	New(tmux, config.Layout{SidebarWidth: 0.4}).Attach("%0")

	assert.Empty(t, tmux.broken)
	assert.Empty(t, tmux.killed)
}

func newDocument(markdown string) Content {
	return Content{
		Key:    "text:" + markdown,
		Wraps:  true,
		Render: func(Target) ([]string, bool) { return []string{"less", markdown}, true },
	}
}

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

type recordingTmux struct {
	width  int
	height int

	splits []string

	anchors      []string
	belowAnchors []string

	resizes []string
	swaps   int

	respawns     []string
	respawnPanes []string

	siblings []string
	owners   map[string]string

	broken []string
	killed []string

	deadPanes map[string]bool
}

func (t *recordingTmux) IsPaneAlive(pane string) bool { return !t.deadPanes[pane] }
func (t *recordingTmux) GetPaneWidth(string) int      { return t.width }
func (t *recordingTmux) GetPaneHeight(string) int     { return t.height }

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
