//go:build integration

package layout

import (
	"strconv"
	"testing"
	"time"

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
	syncDivider(t, frame, total-51)

	width, measured := frame.SidebarWidth(total)
	assert.Equal(t, 50, width, "a dragged divider is what the sidebar draws in")
	assert.True(t, measured)

	frame.Solo(true)
	require.Equal(t, total, paneWidth(t, server, sidebar), "the zoomed sidebar has the whole window")
	frame.SyncSplit()

	width, _ = frame.SidebarWidth(total)
	assert.Equal(t, 50, width, "and holding the window leaves the divider where it was")
}

// TestADeadStagePaneIsReplacedAtTheDraggedWidth keeps the dragged sidebar width.
func TestADeadStagePaneIsReplacedAtTheDraggedWidth(t *testing.T) {
	frame, server, sidebar := realFrame(t)

	stage := frame.Pane(RegionStage)
	total := paneWidth(t, server, sidebar) + paneWidth(t, server, stage) + 1

	server.Run("resize-pane", "-t", sidebar, "-x", "50")
	syncDivider(t, frame, total-51)
	require.Equal(t, 50, paneWidth(t, server, sidebar), "the divider is dragged off the configured width")

	server.Run("kill-pane", "-t", stage)
	frame.Show(RegionStage, Content{
		Key:   "back-to-diff",
		Wraps: true,
		Render: func(Target) ([]string, bool) {
			return []string{"sh", "-c", "sleep 10"}, true
		},
	})

	replaced := frame.Pane(RegionStage)
	require.NotEmpty(t, replaced, "the replacement stage was opened")
	require.NotEqual(t, stage, replaced, "the dead pane was replaced")
	assert.Equal(t, 50, paneWidth(t, server, sidebar), "and the sidebar is left where it was dragged")

	width, measured := frame.SidebarWidth(total)
	assert.Equal(t, 50, width, "so what the panel draws in agrees with what tmux gave it")
	assert.True(t, measured)
}

// TestAResizeWhileTheStagePaneIsDeadKeepsTheDivider covers the resize tmux delivers when the
// stage pane dies and its columns go to the sidebar.
func TestAResizeWhileTheStagePaneIsDeadKeepsTheDivider(t *testing.T) {
	frame, server, sidebar := realFrame(t)

	stage := frame.Pane(RegionStage)
	total := paneWidth(t, server, sidebar) + paneWidth(t, server, stage) + 1

	server.Run("resize-pane", "-t", sidebar, "-x", "50")
	syncDivider(t, frame, total-51)

	server.Run("kill-pane", "-t", stage)
	require.Equal(t, total, frame.WindowCols(), "the window is the size it always was")

	frame.SyncSplit()

	frame.Show(RegionStage, Content{
		Key:   "back-to-diff",
		Wraps: true,
		Render: func(Target) ([]string, bool) {
			return []string{"sh", "-c", "sleep 10"}, true
		},
	})

	require.NotEqual(t, stage, frame.Pane(RegionStage), "the dead pane was replaced")
	assert.Equal(t, 50, paneWidth(t, server, sidebar), "and the sidebar is left where it was dragged")

	width, measured := frame.SidebarWidth(total)
	assert.Equal(t, 50, width, "so what the panel draws in agrees with what tmux gave it")
	assert.True(t, measured)
}

func paneWidth(t *testing.T, server *tmuxtest.Server, pane string) int {
	t.Helper()

	width, err := strconv.Atoi(server.Run("display-message", "-p", "-t", pane, "#{pane_width}"))
	require.NoError(t, err)

	return width
}

func syncDivider(t *testing.T, frame *Frame, want int) {
	t.Helper()

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		frame.SyncSplit()
		assert.Equal(collect, want, frame.StageCols())
	}, time.Second, 10*time.Millisecond)
}
