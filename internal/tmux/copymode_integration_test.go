//go:build integration

package tmux

import (
	"os"
	"os/exec"
	"path/filepath"
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

	client.TuneCopyMode("fg=colour0,bg=colour5")

	assert.Equal(t, "vi", client.ShowGlobal("mode-keys"))
	assert.Equal(t, "fg=colour0,bg=colour5", client.ShowGlobal("mode-style"), "a selection is drawn in the visual mode's colour")
	assert.Equal(t, "on", client.ShowGlobal("set-clipboard"))
	if _, err := client.query("show-options", "-g", "copy-mode-position-format"); err == nil {
		assert.Empty(t, client.ShowGlobal("copy-mode-position-format"), "the sidebar names the mode, not the pane")
	}

	bindings, err := client.getKeys(copyModeTable)
	require.NoError(t, err)
	assert.Equal(t, "send-keys -X begin-selection", bindings["v"], "v starts a selection as it does in vim")
	assert.Equal(t, `send-keys -X begin-selection \; send-keys -X rectangle-on`, bindings["C-v"], "ctrl+v starts a block")
	for _, key := range []string{"y", "Enter", "C-j", "D", "MouseDragEnd1Pane", "DoubleClick1Pane", "TripleClick1Pane"} {
		assert.Contains(t, bindings[key], "copy-pipe-and-cancel", "%s copies", key)
	}
	assert.Contains(t, bindings["Escape"], "clear-selection", "esc drops a selection before it leaves copy-mode")
}

// TestIntegrationCopyStandInHoldsThePaneSlotUntilCopyModeEnds pins the stand-in's life: it takes the
// pane's slot with the block holding the keys in copy-mode, copies only that block's cells, and on
// leaving copy-mode gives the slot and the keys back and goes.
func TestIntegrationCopyStandInHoldsThePaneSlotUntilCopyModeEnds(t *testing.T) {
	tests := []struct {
		name   string
		zoomed bool
		yank   bool
	}{
		{name: "split"},
		{name: "zoomed", zoomed: true},
		{name: "yanked", yank: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testServer(t, "standin")
			client.TuneCopyMode("fg=colour0,bg=colour5")
			client.SetGlobal("copy-command", "")
			_, err := client.query("set-environment", "-g", "PATH", buildStandInPath(t))
			require.NoError(t, err)

			sidebar, err := client.paneID("standin:0.0")
			require.NoError(t, err)

			_, err = client.query("split-window", "-h", "-t", sidebar, "--", "sh")
			require.NoError(t, err)
			client.SelectPane(sidebar)

			if tt.zoomed {
				_, err = client.query("resize-pane", "-Z", "-t", sidebar)
				require.NoError(t, err)
			}

			window := client.GetWindowID(sidebar)
			before := client.GetShownPanes(sidebar)

			binary, err := os.Executable()
			require.NoError(t, err)

			require.NoError(t, client.OpenCopyStandIn(panes.CopyStandIn{
				Pane:        sidebar,
				Binary:      binary,
				BorderStyle: "fg=colour8",
				Rows:        []string{"list│one", "list│two", "list│三x"},
				Divider:     4,
				Rule:        -1,
				RuleLeft:    true,
				Focus:       panes.Rect{Left: 5, Width: 3, Height: 3},
				Row:         1,
				Col:         5,
				Keys:        []string{"x", "y"},
			}))

			var standIn string
			require.Eventually(t, func() bool {
				standIn = client.GetActivePane(window)

				return standIn != sidebar && standIn != ""
			}, 5*time.Second, 20*time.Millisecond, "the stand-in takes the keys")

			inner := &Client{server: []string{"-L", "komrad-copy-" + paneOption(t, client, standIn, "pane_pid"), "-f", os.DevNull}}
			require.Eventually(t, func() bool {
				return inner.value("display-message", "-p", "-t", "%1", "#{pane_in_mode} #{copy_cursor_x} #{copy_cursor_y}") == "1 0 1"
			}, 5*time.Second, 20*time.Millisecond, "the block holding the keys is in copy-mode at the row and column asked for")

			socket, err := inner.GetSocketPath()
			require.NoError(t, err)
			assert.Equal(t, []string{"list", "list", "list"}, strings.Split(strings.TrimSpace(inner.CapturePane("%0")), "\n"), "the list is drawn in a tile of its own")
			assert.Contains(t, inner.CapturePane("%1"), "two", "the block is drawn without the list beside it")
			assert.NotContains(t, inner.CapturePane("%1"), "list")
			assert.Equal(t, slotOf(before, sidebar), slotOf(client.GetShownPanes(standIn), standIn), "the stand-in fills the slot the pane left")
			assert.Equal(t, tt.zoomed, paneOption(t, client, standIn, "window_zoomed_flag") == "1", "the stand-in keeps the window's zoom")
			assert.NotEqual(t, window, client.GetWindowID(sidebar), "the pane waits in another window")

			if tt.yank {
				for _, keys := range [][]string{{"-X", "end-of-line"}, {"-X", "begin-selection"}, {"-X", "cursor-up"}, {"-X", "start-of-line"}, {"y"}} {
					_, err := inner.query(append([]string{"send-keys", "-t", "%1"}, keys...)...)
					require.NoError(t, err)
				}

				assert.EventuallyWithT(t, func(c *assert.CollectT) {
					copied, err := client.query("show-buffer")
					assert.NoError(c, err)
					assert.Equal(c, "one\ntwo", copied)
				}, 5*time.Second, 20*time.Millisecond, "a copy holds only the block's cells, in the outer server's buffer")
			} else {
				_, err := inner.query("send-keys", "-t", "%1", "-X", "cancel")
				require.NoError(t, err)
			}

			require.Eventually(t, func() bool {
				return client.GetWindowID(sidebar) == window && client.GetActivePane(window) == sidebar
			}, 5*time.Second, 20*time.Millisecond, "the pane is swapped back and given the keys")

			assert.Equal(t, tt.zoomed, paneOption(t, client, sidebar, "window_zoomed_flag") == "1", "the pane gets the window back zoomed as it left it")

			require.Eventually(t, func() bool {
				return strings.Contains(client.CapturePane(sidebar), "xy")
			}, 5*time.Second, 20*time.Millisecond, "the pane is sent the keys")

			assert.Equal(t, []string{"standin"}, windowNames(t, client, "standin"), "the stand-in is gone")
			assert.Eventually(t, func() bool {
				_, err := os.Stat(socket)

				return os.IsNotExist(err)
			}, 5*time.Second, 20*time.Millisecond, "the stand-in's server and its socket are gone")
		})
	}
}

// buildStandInPath is a PATH holding only what a stand-in runs.
func buildStandInPath(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, name := range []string{"tmux", "sh", "cat", "rm", "sleep"} {
		path, err := exec.LookPath(name)
		require.NoError(t, err)
		require.NoError(t, os.Symlink(path, filepath.Join(dir, name)))
	}

	return dir
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
