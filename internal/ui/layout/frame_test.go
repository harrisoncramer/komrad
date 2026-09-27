package layout

import (
	"strconv"
	"strings"
	"testing"

	"komrad/config"
	"komrad/internal/engine"
	"komrad/internal/panes"

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
			want:   []string{"right %0 50%", "below %1 40%"},
		},
		{
			name:   "a sidebar on the right leaves the stage the left",
			layout: config.Layout{SidebarSide: config.SidebarSideRight},
			want:   []string{"left %0 50%", "below %1 40%"},
		},
		{
			name:   "an info pane above opens over the stage",
			layout: config.Layout{InfoSide: config.InfoSideAbove},
			want:   []string{"right %0 50%", "above %1 40%"},
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

// TestFrameSizesTheNavPaneInColumnsAgainAfterARebuild covers the stage losing its pane.
func TestFrameSizesTheNavPaneInColumnsAgainAfterARebuild(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.Layout{SidebarWidth: 40})
	frame.Attach("%0")

	tmux.deadPanes = map[string]bool{frame.Pane(RegionStage): true}
	frame.Show(RegionStage, newDocument("# rebuilt"))

	require.Len(t, tmux.splits, 2, "the dead stage was split again")
	assert.Equal(t, []string{"%0 40", "%0 40"}, tmux.resizes)
}

// TestFrameShowMeasuresThePane covers the width content is laid out to.
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

// TestFrameShowKeylessContentAlwaysDraws covers content that names nothing it could be compared
// against.
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

// TestFrameShowRefusesTheNavRegion covers the one pane the frame will not fill.
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
	frame.ShowGuest(Guest{Agent: "agent-9", Pane: "%9"})

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

	frame.ShowGuest(Guest{Agent: "agent-9", Pane: "%9"})

	assert.Equal(t, "%9", frame.GuestPane())
	assert.Equal(t, "%1", frame.Pane(RegionStage), "the region keeps its own pane, away in the agent's window")
	require.Equal(t, 1, tmux.swaps)

	frame.ShowGuest(Guest{Agent: "agent-9", Pane: "%9"})
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
	frame.ShowGuest(Guest{Agent: "agent-9", Pane: "%9"})
	frame.Show(RegionStage, newDocument("# auth"))

	assert.Len(t, tmux.respawns, 2)
}

// TestFrameReleaseGuest covers an agent about to be killed, whose pane leaves the stage first
// so the region is not left minding it.
func TestFrameReleaseGuest(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")
	frame.ShowGuest(Guest{Agent: "agent-9", Pane: "%9"})

	frame.ReleaseGuest("%8")
	assert.Equal(t, "%9", frame.GuestPane(), "another agent's pane is not the one staged")

	frame.ReleaseGuest("%9")
	assert.Empty(t, frame.GuestPane())
}

// TestFrameGuestOutlivingTheStageGoesHome covers the stage's pane dying while an agent is swapped
// in, which leaves the agent standing in the panel's window with nothing to swap it back with.
func TestFrameGuestOutlivingTheStageGoesHome(t *testing.T) {
	tmux := &recordingTmux{deadPanes: map[string]bool{}}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")
	frame.ShowGuest(Guest{Agent: "agent-9", Pane: "%9"})
	require.Equal(t, 1, tmux.swaps)

	tmux.deadPanes["%1"] = true
	frame.Show(RegionStage, newDocument("# auth"))

	assert.Equal(t, []string{"%9"}, tmux.broken, "the agent is returned to a window of its own")
	assert.Equal(t, 1, tmux.swaps, "rather than swapped with a pane that is gone")
	assert.Empty(t, frame.GuestPane())
}

// TestFrameVacateLeavesOnlyTheSidebar covers the panel on its way out, where a staged agent goes
// back to its own window and the frame's own panes die rather than outliving the panel.
func TestFrameVacateLeavesOnlyTheSidebar(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%0")
	frame.ShowGuest(Guest{Agent: "agent-9", Pane: "%9"})
	frame.Show(RegionInfo, newDocument("# auth"))
	require.Equal(t, 1, tmux.swaps)

	frame.Vacate()

	assert.Equal(t, 2, tmux.swaps, "the guest is swapped back to where it came from")
	assert.Empty(t, frame.GuestPane())
	assert.ElementsMatch(t, []string{"%1", "%2"}, tmux.killed, "the stage and the info pane are killed")
	assert.False(t, frame.IsOpen(RegionStage))
	assert.False(t, frame.IsOpen(RegionInfo))
}

// TestFrameClaimedForgetsWhatWasDrawn covers a region another process has taken over, which
// is how a script that fills the pane itself says the frame no longer knows what is in it.
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

	New(tmux, config.Layout{SidebarWidth: 40}).Attach("%0")

	assert.Equal(t, []string{"%7"}, tmux.broken, "a staged agent is returned to its own window")
	assert.Equal(t, []string{"%8"}, tmux.killed, "the last frame's own pane is not kept")
}

// TestFrameAttachLeavesAnEmptyWindowAlone covers the ordinary first start, where there is
// nothing to reclaim.
func TestFrameAttachLeavesAnEmptyWindowAlone(t *testing.T) {
	tmux := &recordingTmux{}

	New(tmux, config.Layout{SidebarWidth: 40}).Attach("%0")

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
	width        int
	windowWidth  int
	height       int
	splits       []string
	anchors      []string
	belowAnchors []string
	resizes      []string
	swaps        int
	respawns     []string
	respawnPanes []string
	respawnDirs  []string
	siblings     []string
	owners       map[string]string
	broken       []string
	killed       []string
	deadPanes    map[string]bool
	zoomed       bool
	// keysElsewhere is the keyboard sitting in a pane other than the one being asked about.
	keysElsewhere bool
	zooms         []bool
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

func (t *recordingTmux) IsPaneAlive(pane string) bool { return !t.deadPanes[pane] }
func (t *recordingTmux) GetPaneWidth(string) int      { return t.width }
func (t *recordingTmux) GetWindowWidth(string) int    { return t.windowWidth }
func (t *recordingTmux) SetPaneZoomed(_ string, on bool) {
	t.zoomed = on
	t.zooms = append(t.zooms, on)
}

// GetPaneFocus answers the way tmux was measured to: a pane the keys have moved off is neither
// the active one nor zoomed any more.
func (t *recordingTmux) GetPaneFocus(string) (focused, zoomed bool) {
	if t.keysElsewhere {
		return false, false
	}

	return true, t.zoomed
}
func (t *recordingTmux) GetPaneHeight(string) int { return t.height }

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

// recordSplit answers with the pane a split left, dropping the zoom the way tmux does, which only
// respawn-pane leaves standing.
func (t *recordingTmux) recordSplit(direction, anchor, size string) string {
	t.splits = append(t.splits, strings.Join([]string{direction, anchor, size}, " "))
	t.zoomed = false

	if direction == "left" || direction == "right" {
		t.anchors = append(t.anchors, anchor)

		return "%1"
	}

	t.belowAnchors = append(t.belowAnchors, anchor)

	return "%2"
}

func (t *recordingTmux) ResizePane(pane, width string) {
	t.resizes = append(t.resizes, pane+" "+width)
	t.zoomed = false
}

func (t *recordingTmux) SwapPanes(string, string) {
	t.swaps++
	t.zoomed = false
}

func (t *recordingTmux) RespawnPane(pane, dir string, argv ...string) {
	t.respawns = append(t.respawns, strings.Join(argv, " "))
	t.respawnPanes = append(t.respawnPanes, pane)
	t.respawnDirs = append(t.respawnDirs, dir)
}

func (t *recordingTmux) GetSiblingPanes(string) []string { return t.siblings }
func (t *recordingTmux) GetPaneOwner(pane string) string { return t.owners[pane] }

func (t *recordingTmux) BreakPane(pane string) {
	t.broken = append(t.broken, pane)
	t.zoomed = false
}

func (t *recordingTmux) KillPane(pane string) {
	t.killed = append(t.killed, pane)
	t.zoomed = false
}

// TestFrameSoloTakesTheWholeWindow covers a screen that draws its own split: the panes it was
// sharing with are covered rather than closed, and giving the window back uncovers them.
func TestFrameSoloTakesTheWholeWindow(t *testing.T) {
	tmux := &recordingTmux{}
	frame := New(tmux, config.Layout{SidebarWidth: 40})
	frame.Attach("%0")

	stage := frame.Pane(RegionStage)
	tmux.zooms = nil

	frame.Solo(true)
	assert.True(t, frame.IsSolo())
	assert.True(t, tmux.zoomed)
	assert.Equal(t, stage, frame.Pane(RegionStage), "the pane underneath is still the frame's")

	frame.Solo(false)
	assert.False(t, frame.IsSolo())
	assert.Equal(t, []bool{true, false}, tmux.zooms)
}

// TestFrameAttachGivesTheWindowBack covers a panel that died holding the window.
func TestFrameAttachGivesTheWindowBack(t *testing.T) {
	tmux := &recordingTmux{zoomed: true}

	New(tmux, config.Layout{SidebarWidth: 40}).Attach("%0")

	assert.False(t, tmux.zoomed)
}

// TestFrameSidebarWidthFallsBackToTheConfiguredColumns covers the columns a screen holding the
// window lays its own split out against before the divider has been measured.
func TestFrameSidebarWidthFallsBackToTheConfiguredColumns(t *testing.T) {
	tests := []struct {
		name  string
		width int
		total int
		want  int
	}{
		{
			name:  "a count of columns is taken as it is",
			width: 40,
			total: 200,
			want:  40,
		},
		{
			name:  "and never more than the window holds",
			width: 400,
			total: 200,
			want:  200,
		},
		{
			name:  "an unmeasured window has no columns to divide",
			width: 40,
			total: 0,
			want:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			frame := New(&recordingTmux{}, config.Layout{SidebarWidth: tt.width})

			width, measured := frame.SidebarWidth(tt.total)
			assert.Equal(t, tt.want, width)
			assert.False(t, measured, "nothing has been measured, so this is the configured width")
		})
	}
}

// TestFrameSidebarWidthFollowsTheDivider covers a divider the user has dragged: the sidebar is
// measured off the pane beside it, which a zoom leaves alone.
func TestFrameSidebarWidthFollowsTheDivider(t *testing.T) {
	tmux := &recordingTmux{width: 120}
	frame := New(tmux, config.Layout{SidebarWidth: 40})
	frame.Attach("%0")

	width, measured := frame.SidebarWidth(200)
	assert.Equal(t, 79, width, "the window less the stage and the divider")
	assert.True(t, measured)

	tmux.width = 160
	frame.SyncSplit()

	width, measured = frame.SidebarWidth(200)
	assert.Equal(t, 39, width, "a dragged divider is followed on the next sync")
	assert.True(t, measured)

	tmux.width = 0
	frame.SyncSplit()

	width, measured = frame.SidebarWidth(200)
	assert.Equal(t, 39, width, "and a pane too dead to measure leaves the divider where it was")
	assert.True(t, measured)

	frame.ForgetSplit()

	width, measured = frame.SidebarWidth(200)
	assert.Equal(t, 40, width, "only forgetting the divider gives the configured width back")
	assert.False(t, measured, "which the caller is told is the configured one")
}

// TestRestoreSplitGivesTheSidebarItsColumnsBack covers a window that changed size, where tmux
// shares the change out between the panes and leaves a narrow sidebar with almost none of it.
func TestRestoreSplitGivesTheSidebarItsColumnsBack(t *testing.T) {
	tmux := &recordingTmux{width: 200, height: 50}
	frame := New(tmux, config.Layout{SidebarWidth: 16})
	frame.Attach("%0")
	frame.ShowGuest(Guest{Agent: "agent-7", Pane: "%7"})

	tmux.resizes = nil
	frame.RestoreSplit()

	assert.Contains(t, tmux.resizes, frame.Pane(RegionSidebar)+" 16",
		"the sidebar is put back to the width it is configured for")
}

// TestRestoreSplitLeavesAZoomedSidebarAlone covers the sidebar holding the whole window, where
// resizing the pane beside it would fight the zoom rather than restore a split.
func TestRestoreSplitLeavesAZoomedSidebarAlone(t *testing.T) {
	tmux := &recordingTmux{width: 200, height: 50}
	frame := New(tmux, config.Layout{SidebarWidth: 16})
	frame.Attach("%0")
	frame.ShowGuest(Guest{Agent: "agent-7", Pane: "%7"})
	frame.Solo(true)

	tmux.resizes = nil
	frame.RestoreSplit()

	assert.Empty(t, tmux.resizes, "nothing is resized while the sidebar holds the window")

	frame.Solo(false)

	assert.Equal(t, []string{frame.Pane(RegionSidebar) + " 16"}, tmux.resizes,
		"and the split is put right once it lets the window go")
}

// TestRestoreSplitKeepsAChosenWidth covers a window that changed size after the divider was
// dragged, which puts the sidebar back where it was dragged to rather than at the config.
func TestRestoreSplitKeepsAChosenWidth(t *testing.T) {
	tmux := &recordingTmux{
		width:       140,
		windowWidth: 200,
		height:      50,
	}
	frame := New(tmux, config.Layout{SidebarWidth: 16})
	frame.Attach("%0")
	frame.ShowGuest(Guest{Agent: "agent-7", Pane: "%7"})
	frame.SyncSplit()

	tmux.resizes = nil
	frame.RestoreSplit()

	assert.Equal(t, []string{frame.Pane(RegionSidebar) + " 59"}, tmux.resizes)
}

// TestFrameNudgeResizesTheSharedSplit covers a key moving the edge while tmux draws the divider.
func TestFrameNudgeResizesTheSharedSplit(t *testing.T) {
	tmux := &recordingTmux{
		width:       160,
		windowWidth: 200,
		height:      50,
	}
	frame := New(tmux, config.Layout{SidebarWidth: 40})
	frame.Attach("%0")

	tmux.resizes = nil
	frame.Nudge(4)

	assert.Equal(t, []string{"%0 43"}, tmux.resizes)

	width, measured := frame.SidebarWidth(200)
	assert.Equal(t, 43, width)
	assert.True(t, measured)
}

// TestFrameNudgeWhileZoomedIsPaidOnUnzoom covers a key moving the edge the panel paints itself,
// where a resize would unzoom the window and so waits for the zoom to be let go.
func TestFrameNudgeWhileZoomedIsPaidOnUnzoom(t *testing.T) {
	tmux := &recordingTmux{
		width:       160,
		windowWidth: 200,
		height:      50,
	}
	frame := New(tmux, config.Layout{SidebarWidth: 40})
	frame.Attach("%0")
	frame.Solo(true)

	tmux.resizes = nil
	frame.Nudge(-8)
	frame.Nudge(-8)

	assert.Empty(t, tmux.resizes, "nothing is resized under the zoom")

	width, _ := frame.SidebarWidth(200)
	assert.Equal(t, 23, width, "the painted split follows at once")

	frame.SyncSplit()
	width, _ = frame.SidebarWidth(200)
	assert.Equal(t, 23, width, "and the stale pane under the zoom does not take it back")

	frame.Solo(false)

	assert.Equal(t, []string{"%0 23"}, tmux.resizes, "letting the zoom go sets the split")
	assert.False(t, tmux.zoomed)
}

// TestFrameNudgeIsPaidWhenTmuxLetsTheZoomGo covers the zoom being dropped by tmux rather than by
// the panel, which a sync finds.
func TestFrameNudgeIsPaidWhenTmuxLetsTheZoomGo(t *testing.T) {
	tmux := &recordingTmux{
		width:       160,
		windowWidth: 200,
		height:      50,
	}
	frame := New(tmux, config.Layout{SidebarWidth: 40})
	frame.Attach("%0")
	frame.Solo(true)
	frame.Nudge(10)

	tmux.resizes = nil
	tmux.zoomed = false
	frame.SyncKeys()

	assert.Equal(t, []string{"%0 49"}, tmux.resizes)
}

// TestFrameNudgeLeavesBothHalvesAColumn covers a key pressed past either end of the window.
func TestFrameNudgeLeavesBothHalvesAColumn(t *testing.T) {
	tmux := &recordingTmux{
		width:       160,
		windowWidth: 200,
		height:      50,
	}
	frame := New(tmux, config.Layout{SidebarWidth: 40})
	frame.Attach("%0")

	frame.Nudge(-500)
	width, _ := frame.SidebarWidth(200)
	assert.Equal(t, 1, width)

	frame.Nudge(500)
	width, _ = frame.SidebarWidth(200)
	assert.Equal(t, 198, width)
}

// TestAWindowTooNarrowForTheChosenWidthKeepsIt covers tmux clamping the sidebar into a window
// narrower than the width chosen for it, which is not a choice.
func TestAWindowTooNarrowForTheChosenWidthKeepsIt(t *testing.T) {
	tmux := &recordingTmux{
		width:       149,
		windowWidth: 200,
		height:      50,
	}
	frame := New(tmux, config.Layout{SidebarWidth: 16})
	frame.Attach("%0")
	frame.SyncSplit()

	// tmux leaves the stage its one-column minimum and gives the sidebar the rest.
	tmux.windowWidth, tmux.width = 40, 1
	frame.RestoreSplit()
	frame.SyncSplit()

	tmux.windowWidth, tmux.width = 200, 100
	tmux.resizes = nil
	frame.RestoreSplit()

	assert.Equal(t, []string{"%0 50"}, tmux.resizes, "the width chosen before the window shrank")
}

// remoteEngine is an engine somewhere this tmux server cannot reach.
func remoteEngine() engine.Remote {
	return engine.Remote{Destination: "komrad@host"}
}

// newRemoteFrame is a frame over an engine somewhere else, reached with komrad terminal.
func newRemoteFrame(tmux *recordingTmux) *Frame {
	return New(tmux, config.DefaultLayout(),
		WithEngine(remoteEngine()),
		WithTerminalCommand([]string{"komrad", "terminal", "--addr", "127.0.0.1:19001"}))
}

// TestFocusPaneIsTheGuestForALocalEngine covers the focus key jumping to the agent's own pane,
// which is the pane that was moved into the stage.
func TestFocusPaneIsTheGuestForALocalEngine(t *testing.T) {
	tmux := &recordingTmux{width: 80, height: 40}
	frame := New(tmux, config.DefaultLayout())
	frame.Attach("%1")
	frame.ShowGuest(Guest{Agent: "agent-7", Pane: "%7"})

	assert.Equal(t, "%7", frame.FocusPane())
}

// TestFocusPaneIsTheStageForARemoteEngine covers an agent's pane belonging to another server,
// where the only pane the focus key can land on is the stage attached to it and without which
// the key is unbound.
func TestFocusPaneIsTheStageForARemoteEngine(t *testing.T) {
	tmux := &recordingTmux{width: 80, height: 40}
	frame := newRemoteFrame(tmux)
	frame.Attach("%1")

	require.Empty(t, frame.FocusPane(), "nothing is staged yet")

	frame.ShowGuest(Guest{Agent: "agent-7", Pane: "%7"})

	stage := frame.Pane(RegionStage)
	require.NotEmpty(t, stage)
	assert.Equal(t, stage, frame.FocusPane())
	assert.NotEqual(t, "%7", frame.FocusPane(), "the agent's pane belongs to the engine")
}

// TestRemoteGuestIsNotTrackedAsAGuest covers the swap-back path never being handed a pane this
// server does not hold.
func TestRemoteGuestIsNotTrackedAsAGuest(t *testing.T) {
	tmux := &recordingTmux{width: 80, height: 40}
	frame := newRemoteFrame(tmux)
	frame.Attach("%1")
	frame.ShowGuest(Guest{Agent: "agent-7", Pane: "%7"})

	assert.Empty(t, frame.GuestPane())
}

// TestFocusPaneIsEmptyOnceTheStageShowsSomethingElse covers the key being given back when the stage
// stops holding an agent.
func TestFocusPaneIsEmptyOnceTheStageShowsSomethingElse(t *testing.T) {
	tmux := &recordingTmux{width: 80, height: 40}
	frame := newRemoteFrame(tmux)
	frame.Attach("%1")
	frame.ShowGuest(Guest{Agent: "agent-7", Pane: "%7"})
	require.NotEmpty(t, frame.FocusPane())

	frame.Show(RegionStage, Content{
		Key:    "blank",
		Render: func(Target) ([]string, bool) { return []string{"true"}, true },
	})

	assert.Empty(t, frame.FocusPane())
}

// TestRemoteGuestIsAttachedAgainOnceItsViewerDies covers the ssh in the stage dropping while the
// cursor stays on the same agent, which takes the stage pane with it.
func TestRemoteGuestIsAttachedAgainOnceItsViewerDies(t *testing.T) {
	tmux := &recordingTmux{
		width:     80,
		height:    40,
		deadPanes: map[string]bool{},
	}
	frame := newRemoteFrame(tmux)
	frame.Attach("%1")
	frame.ShowGuest(Guest{Agent: "agent-7", Pane: "%7"})

	stage := frame.Pane(RegionStage)
	require.NotEmpty(t, stage)
	require.Len(t, tmux.respawns, 1)

	frame.ShowGuest(Guest{Agent: "agent-7", Pane: "%7"})
	require.Len(t, tmux.respawns, 1, "the same agent is not attached twice")

	tmux.deadPanes[stage] = true

	frame.ShowGuest(Guest{Agent: "agent-7", Pane: "%7"})

	assert.Len(t, tmux.respawns, 2, "the viewer is run again in a fresh stage")
	assert.Equal(t, frame.Pane(RegionStage), frame.FocusPane())
}

// TestRemoteGuestAttachesOverTheAPI covers what the stage runs to show an agent on an engine
// somewhere else.
func TestRemoteGuestAttachesOverTheAPI(t *testing.T) {
	tests := []struct {
		name    string
		command []string
		guest   Guest
		want    []string
	}{
		{
			name:    "the stage runs komrad terminal on the agent",
			command: []string{"komrad", "terminal", "--addr", "127.0.0.1:19001"},
			guest:   Guest{Agent: "agent-7", Pane: "%7"},
			want:    []string{"komrad terminal --addr 127.0.0.1:19001 -- agent-7"},
		},
		{
			name:  "an engine the panel cannot reach leaves the stage alone",
			guest: Guest{Agent: "agent-7", Pane: "%7"},
		},
		{
			name:    "a guest with no agent leaves the stage alone",
			command: []string{"komrad", "terminal", "--addr", "127.0.0.1:19001"},
			guest:   Guest{Pane: "%7"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmux := &recordingTmux{width: 80, height: 40}
			frame := New(tmux, config.DefaultLayout(), WithEngine(remoteEngine()), WithTerminalCommand(tt.command))
			frame.Attach("%1")
			frame.ShowGuest(tt.guest)

			assert.Equal(t, tt.want, tmux.respawns)
		})
	}
}
