package ui_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVisualMode covers leader v in the sidebar handing tmux's copy-mode a stand-in holding only
// the block that has the keys.
func TestVisualMode(t *testing.T) {
	tests := []struct {
		name       string
		from       []string
		wantCursor string
		wantAbsent string
	}{
		{
			name:       "the list is handed over with the cursor on its row",
			wantCursor: "a1",
		},
		{
			name:       "the record is handed over without the list above it",
			from:       []string{"pick:record"},
			wantCursor: "name",
			wantAbsent: "komrad › agents",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openFocus(t)
			harness.press(tt.from...)

			harness.press("ctrl+space", "v")

			standIns := harness.panel.ReadStandIns()
			require.Len(t, standIns, 1)
			assert.Equal(t, "%0", standIns[0].Pane)
			assert.Equal(t, []string{"C-Space", "v"}, standIns[0].Keys, "the stand-in says when copy-mode ends")

			rows := strings.Split(ansi.Strip(standIns[0].Text), "\n")
			require.GreaterOrEqual(t, len(rows), standIns[0].Row)
			assert.Contains(t, rows[standIns[0].Row-1], "› ")
			assert.Contains(t, rows[standIns[0].Row-1], tt.wantCursor)
			if tt.wantAbsent != "" {
				assert.NotContains(t, standIns[0].Text, tt.wantAbsent)
			}
		})
	}

	t.Run("copy-mode ending lets the window move again", func(t *testing.T) {
		harness := openFocus(t)

		harness.press("ctrl+space", "v")
		harness.press("ctrl+space", "j")
		assert.NotContains(t, harness.readDrawn(), "PANES", "the picker waits while the stand-in has the sidebar's place")

		harness.press("ctrl+space", "v")
		harness.press("ctrl+space", "j")
		assert.Contains(t, harness.readDrawn(), "PANES")
		assert.Len(t, harness.panel.ReadStandIns(), 1)
	})

	t.Run("a stand-in tmux refuses says so and leaves v to try again", func(t *testing.T) {
		harness := openFocus(t)
		harness.panel.RefuseStandIns(errors.New("no space for a window"))

		harness.press("ctrl+space", "v")
		harness.fireTimers()

		opened := readOpenMessages(harness)
		assert.Contains(t, opened, "Could not start visual mode")
		assert.Contains(t, opened, "no space for a window")

		harness.panel.RefuseStandIns(nil)
		harness.press("ctrl+space", "v")
		assert.Len(t, harness.panel.ReadStandIns(), 2)
	})
}
