//go:build integration

package tmux

import (
	"strings"
	"testing"
	"time"

	"komrade/internal/panes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationMovePanesReachesTmuxAsOneInvocation pins the batching against a real server, a change sent
// a command at a time being painted a command at a time.
func TestIntegrationMovePanesReachesTmuxAsOneInvocation(t *testing.T) {
	client := testServer(t, "moves")

	second := client.SplitRight("moves:moves.0", t.TempDir(), "50%")
	require.NotEmpty(t, second)

	first := paneAt(t, client, "moves", 0)
	require.NotEqual(t, first, second)

	client.MovePanes([]panes.Move{panes.Swap(first, second)})

	assert.Equal(t, second, paneAt(t, client, "moves", 0), "the swap landed")
}

// TestIntegrationMovePanesLeavesTheZoomWhereTheMoveAsksFor covers the conditional a zoom is asked as, which
// has to land whatever an earlier command in the same invocation did to the zoom.
func TestIntegrationMovePanesLeavesTheZoomWhereTheMoveAsksFor(t *testing.T) {
	client := testServer(t, "zoom")

	require.NotEmpty(t, client.SplitRight("zoom:zoom.0", t.TempDir(), "50%"))

	pane := paneAt(t, client, "zoom", 0)

	tests := []struct {
		name   string
		moves  []panes.Move
		zoomed bool
	}{
		{
			name:   "a pane is given the whole window",
			moves:  []panes.Move{panes.Zoom(pane, true)},
			zoomed: true,
		},
		{
			name:   "and asked for again, which costs nothing and changes nothing",
			moves:  []panes.Move{panes.Zoom(pane, true)},
			zoomed: true,
		},
		{
			name:  "the others are handed their share back",
			moves: []panes.Move{panes.Zoom(pane, false)},
		},
		{
			name:  "and asked for again",
			moves: []panes.Move{panes.Zoom(pane, false)},
		},
		{
			name:   "a zoom lands after another move in the same invocation",
			moves:  []panes.Move{panes.Swap(pane, paneAt(t, client, "zoom", 1)), panes.Zoom(pane, true)},
			zoomed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client.MovePanes(tt.moves)
			assert.Equal(t, tt.zoomed, isZoomed(t, client, "zoom"))
		})
	}
}

// TestIntegrationMovePanesRespawnsAPaneWithItsScrollback pins the second command a respawn stands for, a
// pane's scrollback outliving the process that wrote it.
func TestIntegrationMovePanesRespawnsAPaneWithItsScrollback(t *testing.T) {
	client := testServer(t, "respawn")
	pane := paneAt(t, client, "respawn", 0)

	client.MovePanes([]panes.Move{panes.Respawn(pane, t.TempDir(), "sh", "-c", "printf after; sleep 30")})

	require.Eventually(t, func() bool {
		return strings.Contains(client.CapturePane(pane), "after")
	}, 5*time.Second, 20*time.Millisecond, "the pane runs what the move named")

	assert.Equal(t, "after", strings.TrimSpace(client.CapturePane(pane)),
		"and holds nothing the process before it wrote")
}

// TestIntegrationArgsForMoveSaysNothingAboutAPaneItWasNotGiven pins the empty target, which tmux would read
// as the pane the person is looking at rather than as a move nobody asked for.
func TestIntegrationArgsForMoveSaysNothingAboutAPaneItWasNotGiven(t *testing.T) {
	tests := []struct {
		name string
		move panes.Move
	}{
		{name: "a zoom of nothing", move: panes.Zoom("", true)},
		{name: "a swap of nothing", move: panes.Swap("", "%1")},
		{name: "a respawn of nothing", move: panes.Respawn("", "/tmp")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Empty(t, argsForMove(tt.move))
		})
	}
}

func paneAt(t *testing.T, c *Client, session string, index int) string {
	t.Helper()

	out, err := c.query("list-panes", "-t", session+":", "-F", "#{pane_id}")
	require.NoError(t, err)

	ids := strings.Split(out, "\n")
	require.Greater(t, len(ids), index)

	return ids[index]
}

func isZoomed(t *testing.T, c *Client, session string) bool {
	t.Helper()

	out, err := c.query("display-message", "-p", "-t", session+":", "#{window_zoomed_flag}")
	require.NoError(t, err)

	return strings.TrimSpace(out) == "1"
}
