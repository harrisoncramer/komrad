package ui_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVisualMode covers leader v in the sidebar handing tmux's copy-mode a stand-in drawn as the
// whole sidebar, with the cursor where the block that has the keys has it.
func TestVisualMode(t *testing.T) {
	tests := []struct {
		name       string
		from       []string
		wantCursor string
	}{
		{
			name:       "the list's cursor is where copy-mode starts",
			wantCursor: "a1",
		},
		{
			name:       "the record's cursor is where copy-mode starts",
			from:       []string{"pick:record"},
			wantCursor: "name",
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
			assert.Contains(t, ansi.Strip(standIns[0].Text), "komrad › agents", "the rest of the sidebar stays drawn")

			rows := strings.Split(ansi.Strip(standIns[0].Text), "\n")
			require.GreaterOrEqual(t, len(rows), standIns[0].Row)
			from := ansi.Cut(rows[standIns[0].Row-1], standIns[0].Col-1, ansi.StringWidth(rows[standIns[0].Row-1]))
			assert.True(t, strings.HasPrefix(from, "› "), "the cursor lands on the row cursor, got %q", from)
			assert.Contains(t, from, tt.wantCursor)
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
