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

		harness.press(append(widenListPresses, "O", "z")...)

		lines := splitLines(harness.readFrame())
		require.GreaterOrEqual(t, len(lines), 2)
		for i, want := range []string{"\U000F050C komrad", "agents  search  schedules  archive  worktrees"} {
			list, _, _ := strings.Cut(lines[i], "│")
			assert.Equal(t, want, strings.TrimSpace(list))
		}
		assert.Contains(t, harness.readRawFrame(), paint.Panel.Header().Render("archive"))
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
			press:      []string{"O", "s"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "tab moves to the next view and enter opens it",
			press:      []string{"O", "tab", "enter"},
			wantScreen: searchScreen,
		},
		{
			name:       "shift-tab wraps to the last view",
			press:      []string{"O", "shift+tab", "enter"},
			wantScreen: worktreesScreen,
		},
		{
			name:       "escape closes the switcher where it stands",
			press:      []string{"O", "tab", "esc"},
			wantScreen: agentsScreen,
		},
		{
			name:       "the switcher answers from a view it opened",
			press:      []string{"O", "z", "O", "a"},
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

		harness.press("O")

		assert.Contains(t, readLegend(harness.readFrame()), "a agents • z archive")
	})
}
