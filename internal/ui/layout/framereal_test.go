//go:build integration

package layout

import (
	"strconv"
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

// TestSidebarWidthFollowsADraggedDividerUnderTheWindow covers why the width is measured off the
// stage rather than off the sidebar itself: a zoomed sidebar reports the whole window, so the
// divider the user dragged is only readable from the pane beside it.
func TestSidebarWidthFollowsADraggedDividerUnderTheWindow(t *testing.T) {
	frame, server, sidebar := realFrame(t)

	total := paneWidth(t, server, sidebar) + paneWidth(t, server, frame.Pane(RegionStage)) + 1
	width, _ := frame.SidebarWidth(total)
	require.Equal(t, config.DefaultSidebarWidth, width, "the configured columns to begin with")

	server.Run("resize-pane", "-t", sidebar, "-x", "50")
	frame.SyncSplit()

	width, measured := frame.SidebarWidth(total)
	assert.Equal(t, 50, width, "a dragged divider is what the sidebar draws in")
	assert.True(t, measured)

	frame.Solo(true)
	require.Equal(t, total, paneWidth(t, server, sidebar), "the zoomed sidebar has the whole window")
	frame.SyncSplit()

	width, _ = frame.SidebarWidth(total)
	assert.Equal(t, 50, width, "and holding the window leaves the divider where it was")
}

func paneWidth(t *testing.T, server *tmuxtest.Server, pane string) int {
	t.Helper()

	width, err := strconv.Atoi(server.Run("display-message", "-p", "-t", pane, "#{pane_width}"))
	require.NoError(t, err)

	return width
}
