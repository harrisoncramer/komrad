package layout

import (
	"testing"

	"weave/config"
	"weave/internal/tmuxtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realFrame is the frame laid out on a tmux server of its own, which is the only place the zoom
// these tests are about is real: a fake answers for the zoom from the field weave set.
func realFrame(t *testing.T) (*Frame, *tmuxtest.Server, string) {
	t.Helper()

	server := tmuxtest.New(t)
	server.NewSession("panel")
	sidebar := server.Run("display-message", "-p", "-t", "panel", "#{pane_id}")

	frame := New(server.Client(), config.DefaultLayout())
	frame.Attach(sidebar)

	return frame, server, sidebar
}

// isZoomed is what tmux itself holds about the window, asked of the server rather than of the
// frame, since the two disagreeing is the whole of what these tests are for.
func isZoomed(t *testing.T, server *tmuxtest.Server, pane string) bool {
	t.Helper()

	return server.Run("display-message", "-p", "-t", pane, "#{window_zoomed_flag}") == "1"
}

func blankContent(t *testing.T) Content {
	t.Helper()

	return Content{
		Key:    "blank",
		Dir:    t.TempDir(),
		Render: func(Target) ([]string, bool) { return nil, true },
	}
}

// TestTheStageIsClaimedUnderTheWindow covers the stage being split again under a panel that has
// the window, which is what a pane the user closed themselves leaves to be done: split-window and
// resize-pane both drop a zoom, so the panel would be left in its share of the frame with the
// empty pane it just made beside it.
func TestTheStageIsClaimedUnderTheWindow(t *testing.T) {
	frame, server, sidebar := realFrame(t)

	frame.Solo(true)
	require.True(t, isZoomed(t, server, sidebar), "the sidebar has the window to begin with")

	server.Run("kill-pane", "-t", frame.Pane(RegionStage))
	frame.Show(RegionStage, blankContent(t))

	assert.True(t, frame.IsSolo(), "the frame still means to hold the window")
	assert.True(t, isZoomed(t, server, sidebar), "and tmux is still holding it for the frame")
}

// TestAGuestSwapKeepsTheWindow covers moving onto a running agent's row and back off it, since
// swap-pane unzooms the window and calls into nothing when it does, which left the frame sure of
// a zoom tmux had already dropped.
func TestAGuestSwapKeepsTheWindow(t *testing.T) {
	frame, server, sidebar := realFrame(t)
	guest := server.Spawn("guest", "sleep 60")

	frame.ShowGuest(guest)
	require.Equal(t, guest, frame.GuestPane(), "the agent's own pane stands in the stage")

	frame.Solo(true)
	require.True(t, isZoomed(t, server, sidebar), "and the panel takes the window over it")

	// What the cursor landing on a dormant row does: the guest goes back where it came from and
	// the stage is drawn blank behind the panel.
	frame.Show(RegionStage, blankContent(t))

	assert.Empty(t, frame.GuestPane(), "the guest has gone back to its own window")
	assert.True(t, frame.IsSolo(), "the frame still means to hold the window")
	assert.True(t, isZoomed(t, server, sidebar), "and tmux is still holding it for the frame")
}

// TestAGuestIsShownUnderTheWindow covers the swap the other way, which the archive makes while
// the panel is drawing both its columns itself.
func TestAGuestIsShownUnderTheWindow(t *testing.T) {
	frame, server, sidebar := realFrame(t)
	guest := server.Spawn("guest", "sleep 60")

	frame.Solo(true)
	frame.ShowGuest(guest)

	assert.True(t, frame.IsSolo(), "the frame still means to hold the window")
	assert.True(t, isZoomed(t, server, sidebar), "and tmux is still holding it for the frame")
}

// TestTheInfoPaneIsNotCoveredByTheWindow covers the logs being opened under a panel holding the
// window, since a zoomed sidebar covers the pane they are drawn in.
func TestTheInfoPaneIsNotCoveredByTheWindow(t *testing.T) {
	frame, server, sidebar := realFrame(t)

	frame.Solo(true)
	require.True(t, isZoomed(t, server, sidebar), "the sidebar has the window to begin with")

	info := frame.Open(RegionInfo)

	require.NotEmpty(t, info, "the info pane is open")
	assert.False(t, isZoomed(t, server, sidebar), "and nothing puts the zoom back over it")
}

// TestAWindowGivenUpByHandIsLeftAlone covers the user's own prefix+z, which drops the zoom without
// calling into weave: the refresh tick asks for the content that is already up, and a restore hung
// on the call rather than on what the call moved would take the window back within the second.
func TestAWindowGivenUpByHandIsLeftAlone(t *testing.T) {
	frame, server, sidebar := realFrame(t)
	content := blankContent(t)

	frame.Solo(true)
	frame.Show(RegionStage, content)
	require.True(t, isZoomed(t, server, sidebar), "the panel has the window")

	server.Run("resize-pane", "-Z", "-t", sidebar)
	require.False(t, isZoomed(t, server, sidebar), "which the user takes back by hand")

	frame.Show(RegionStage, content)

	assert.False(t, isZoomed(t, server, sidebar), "and the next tick leaves it where they put it")
}
