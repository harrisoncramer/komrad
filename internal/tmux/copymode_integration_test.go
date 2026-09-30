//go:build integration

package tmux

import (
	"strings"
	"testing"
	"time"

	"komrad/internal/panes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationTuneCopyModeMakesCopyModeVisualMode pins the options and keys that turn copy-mode
// into the panel's visual mode.
func TestIntegrationTuneCopyModeMakesCopyModeVisualMode(t *testing.T) {
	client := testServer(t, "tune")

	client.TuneCopyMode("#[fg=colour0] VISUAL #[default]")

	assert.Equal(t, "vi", client.ShowGlobal("mode-keys"))
	if _, err := client.query("show-options", "-g", "copy-mode-position-format"); err == nil {
		assert.Equal(t, "#[align=right]#[fg=colour0] VISUAL #[default]", client.ShowGlobal("copy-mode-position-format"),
			"a tmux older than the option draws copy-mode's own position instead")
	}

	bindings, err := client.getKeys(copyModeTable)
	require.NoError(t, err)
	assert.Equal(t, "send-keys -X begin-selection", bindings["v"], "v starts a selection as it does in vim")
	assert.Equal(t, `send-keys -X begin-selection \; send-keys -X rectangle-on`, bindings["C-v"], "ctrl+v starts a block")
	assert.Contains(t, bindings["y"], "copy-pipe-and-cancel", "y copies as tmux does outside a stand-in")
	assert.Contains(t, bindings["y"], "pipe-and-cancel 'sh #{"+standInCopyOption+"}", "y in a stand-in copies only from its block")
	assert.Contains(t, bindings["Escape"], "clear-selection", "esc drops a selection before it leaves copy-mode")
}

// TestIntegrationCopyStandInHoldsThePaneSlotUntilCopyModeEnds pins the stand-in's life: it takes the
// pane's slot and the keys in copy-mode, and on leaving copy-mode gives both back and goes.
func TestIntegrationCopyStandInHoldsThePaneSlotUntilCopyModeEnds(t *testing.T) {
	tests := []struct {
		name   string
		zoomed bool
	}{
		{name: "split"},
		{name: "zoomed", zoomed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testServer(t, "standin")

			sidebar, err := client.paneID("standin:0.0")
			require.NoError(t, err)

			_, err = client.query("split-window", "-h", "-t", sidebar, "--", "sh")
			require.NoError(t, err)

			if tt.zoomed {
				_, err = client.query("resize-pane", "-Z", "-t", sidebar)
				require.NoError(t, err)
			}

			window := client.GetWindowID(sidebar)
			before := client.GetShownPanes(sidebar)

			require.NoError(t, client.OpenCopyStandIn(panes.CopyStandIn{
				Pane: sidebar,
				Text: "one\ntwo\nthree",
				Row:  2,
				Col:  3,
				Keys: []string{"x", "y"},
			}))

			var standIn string
			require.Eventually(t, func() bool {
				standIn = client.GetActivePane(window)
				if standIn == sidebar || paneOption(t, client, standIn, "pane_in_mode") != "1" {
					return false
				}

				return paneOption(t, client, standIn, "copy_cursor_y") == "1" && paneOption(t, client, standIn, "copy_cursor_x") == "2"
			}, 5*time.Second, 20*time.Millisecond, "the stand-in takes the keys in copy-mode at the row and column asked for")

			assert.Contains(t, client.CapturePane(standIn), "two")
			assert.Equal(t, slotOf(before, sidebar), slotOf(client.GetShownPanes(standIn), standIn),
				"the stand-in fills the slot the pane left")
			assert.Equal(t, tt.zoomed, paneOption(t, client, standIn, "window_zoomed_flag") == "1",
				"the stand-in keeps the window's zoom")
			assert.NotEqual(t, window, client.GetWindowID(sidebar), "the pane waits in another window")

			_, err = client.query("send-keys", "-t", standIn, "-X", "cancel")
			require.NoError(t, err)

			require.Eventually(t, func() bool {
				return client.GetWindowID(sidebar) == window && client.GetActivePane(window) == sidebar
			}, 5*time.Second, 20*time.Millisecond, "the pane is swapped back and given the keys")

			assert.Equal(t, tt.zoomed, paneOption(t, client, sidebar, "window_zoomed_flag") == "1",
				"the pane gets the window back zoomed as it left it")

			require.Eventually(t, func() bool {
				return strings.Contains(client.CapturePane(sidebar), "xy")
			}, 5*time.Second, 20*time.Millisecond, "the pane is sent the keys")

			assert.Equal(t, []string{"standin"}, windowNames(t, client, "standin"), "the stand-in is gone")
		})
	}
}

// TestIntegrationCopyStandInCutsToBlock pins y in a stand-in keeping the cells of a
// selection that fall inside the block.
func TestIntegrationCopyStandInCutsToBlock(t *testing.T) {
	tests := []struct {
		name string
		keys [][]string
		want string
	}{
		{
			name: "lines",
			keys: [][]string{{"-X", "begin-selection"}, {"-X", "cursor-down"}, {"-X", "end-of-line"}},
			want: "one\ntwo",
		},
		{
			name: "upwards",
			keys: [][]string{{"-X", "cursor-down"}, {"-X", "end-of-line"}, {"-X", "begin-selection"}, {"-X", "cursor-up"}, {"-X", "start-of-line"}},
			want: "one\ntwo",
		},
		{
			name: "rectangle",
			keys: [][]string{{"-X", "begin-selection"}, {"-X", "rectangle-on"}, {"-N", "2", "-X", "cursor-down"}, {"-N", "7", "-X", "cursor-right"}},
			want: "o\nt\n三",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testServer(t, "cut")
			client.TuneCopyMode("VISUAL")
			client.SetGlobal("copy-command", "")

			sidebar, err := client.paneID("cut:0.0")
			require.NoError(t, err)
			window := client.GetWindowID(sidebar)

			require.NoError(t, client.OpenCopyStandIn(panes.CopyStandIn{
				Pane:  sidebar,
				Text:  "list │ one\nlist │ two\nlist │ 三x",
				Row:   1,
				Col:   1,
				Block: panes.Rect{Left: 7, Width: 3, Height: 3},
			}))

			var standIn string
			require.Eventually(t, func() bool {
				standIn = client.GetActivePane(window)

				return standIn != sidebar && paneOption(t, client, standIn, "pane_in_mode") == "1"
			}, 5*time.Second, 20*time.Millisecond)

			for _, keys := range tt.keys {
				_, err := client.query(append([]string{"send-keys", "-t", standIn}, keys...)...)
				require.NoError(t, err)
			}
			_, err = client.query("send-keys", "-t", standIn, "y")
			require.NoError(t, err)

			var copied string
			require.Eventually(t, func() bool {
				copied, err = client.query("show-buffer")

				return err == nil
			}, 5*time.Second, 20*time.Millisecond, "the cut lands in a buffer")
			assert.Equal(t, tt.want, copied)
		})
	}
}

func slotOf(shown []panes.Rect, pane string) panes.Rect {
	for _, rect := range shown {
		if rect.Pane == pane {
			rect.Pane = ""

			return rect
		}
	}

	return panes.Rect{}
}
