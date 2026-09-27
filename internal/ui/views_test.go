package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/ui/paint"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViews covers the switcher drawn over every list and the keys that move between views.
func TestViews(t *testing.T) {
	t.Run("the logo and every view open the frame, the current one lit", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{})

		harness.press(append(widenListPresses, "o", "r")...)

		lines := splitLines(harness.readFrame())
		require.GreaterOrEqual(t, len(lines), 2)
		for i, want := range []string{"\U000F050C komrad", "agents  query  schedules  archive  worktrees"} {
			list, _, _ := strings.Cut(lines[i], "│")
			assert.Equal(t, want, strings.TrimSpace(list))
		}
		lit := paint.Panel.Style(paint.ToneAccent)
		assert.Contains(t, harness.readRawFrame(), lit.Render("a")+lit.Underline(true).Render("r")+lit.Render("chive"), "the view that is up is lit with its jump key underlined")
	})

	t.Run("a pane too short for it draws none", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{height: 16})

		assert.NotContains(t, harness.readFrame(), "komrad")
	})

	tests := []struct {
		name       string
		press      []string
		wantScreen string
	}{
		{
			name:       "a letter jumps to its view",
			press:      []string{"o", "s"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "tab opens the next view",
			press:      []string{"o", "tab"},
			wantScreen: searchScreen,
		},
		{
			name:       "the switcher keeps the keys through each view it opens",
			press:      []string{"o", "tab", "tab"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "shift-tab wraps to the last view",
			press:      []string{"o", "shift+tab"},
			wantScreen: worktreesScreen,
		},
		{
			name:       "escape hands the keys back to the view it stands on",
			press:      []string{"o", "tab", "esc", "x"},
			wantScreen: searchScreen,
		},
		{
			name:       "a letter closes the switcher behind it",
			press:      []string{"o", "r", "tab"},
			wantScreen: archiveScreen,
		},
		{
			name:       "the switcher answers from a view it opened",
			press:      []string{"o", "r", "o", "a"},
			wantScreen: agentsScreen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{})

			harness.press(tt.press...)

			assert.Equal(t, tt.wantScreen, harness.readScreen())
		})
	}

	t.Run("the legend names the switcher's keys while it is open", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{})

		harness.press(append(widenListPresses, "o")...)

		assert.Contains(t, readLegend(harness.readFrame()), "a agents • r archive • esc done")
	})
}
