package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/ui/paneltest"
	"komrad/internal/ui/screen"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViews covers the switcher rendered over every list and the keys that move between views.
func TestViews(t *testing.T) {
	t.Run("a page opened from a view renders the trail back to it beside the logo", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, paneltest.Workflow("w1", "komrad"), paneltest.Reviewer("reviewer", "reviewer", "writer"))
		harness.MoveToRow("reviewer")
		harness.Press("space", "l")

		assert.True(t, strings.HasPrefix(paneltest.Trail(harness.Frame()), "agents › reviewer › logs"))
	})

	tests := []struct {
		name       string
		press      []string
		wantScreen screen.ID
	}{
		{
			name:       "tab opens the next view",
			press:      []string{"tab"},
			wantScreen: screen.Schedules,
		},
		{
			name:       "shift-tab opens the previous view, wrapping to the last",
			press:      []string{"shift+tab"},
			wantScreen: screen.Worktrees,
		},
		{
			name:       "the switcher keeps tab through each view it opens",
			press:      []string{"tab", "tab"},
			wantScreen: screen.Documents,
		},
		{
			name:       "the switcher keeps shift-tab through each view it opens",
			press:      []string{"shift+tab", "shift+tab"},
			wantScreen: screen.Documents,
		},
		{
			name:       "escape hands the keys back to the view it stands on",
			press:      []string{"tab", "esc", "x"},
			wantScreen: screen.Schedules,
		},
		{
			name:       "escape on the agents opens the switcher without moving",
			press:      []string{"esc"},
			wantScreen: screen.Agents,
		},
		{
			name:       "escape on the agents hands tab to the switcher",
			press:      []string{"esc", "tab", "tab"},
			wantScreen: screen.Documents,
		},
		{
			name:       "escape on the schedules opens the switcher",
			press:      []string{"tab", "esc", "esc", "tab"},
			wantScreen: screen.Documents,
		},
		{
			name:       "escape on the documents opens the switcher",
			press:      []string{"tab", "tab", "esc", "esc", "tab"},
			wantScreen: screen.Worktrees,
		},
		{
			name:       "escape on the worktrees opens the switcher",
			press:      []string{"shift+tab", "esc", "esc", "tab"},
			wantScreen: screen.Agents,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, paneltest.Params{})

			harness.Press(tt.press...)

			assert.Equal(t, tt.wantScreen, paneltest.Screen(harness.Frame()))
		})
	}

	t.Run("the list keeps its arrow while the switcher is open", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{})
		harness.Press(widenListPresses...)
		hovered := paneltest.HoveredRow(harness.Frame())
		require.NotEmpty(t, hovered)

		harness.Press("esc")
		require.Contains(t, harness.Frame(), "tab/S-tab switch")
		assert.Equal(t, hovered, paneltest.HoveredRow(harness.Frame()))
	})

	t.Run("a key the switcher has no use for closes it and reaches the view", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{})

		harness.Press(append(widenListPresses, "tab", "G")...)

		frame := harness.Frame()
		assert.Equal(t, screen.Schedules, paneltest.Screen(harness.Frame()))
		assert.NotEmpty(t, paneltest.HoveredRow(frame), "the switcher let the list have the keys")
		assert.Equal(t, "schedules › agent00", paneltest.Trail(frame), "and the list took the key")
	})
}

// TestTrail covers the trail under the switcher, from the view that is up to the row the cursor is
// on.
func TestTrail(t *testing.T) {
	tests := []struct {
		name      string
		press     []string
		wantTrail string
	}{
		{
			name:      "the agents trail runs to the hovered agent",
			press:     []string{"G"},
			wantTrail: "agents › agent39",
		},
		{
			name:      "the schedules trail runs to the hovered schedule",
			press:     []string{"tab", "G"},
			wantTrail: "schedules › agent00",
		},
		{
			name:      "the archived agents trail runs to the hovered agent",
			press:     []string{"F", "tab", "esc", "G"},
			wantTrail: "agents › agent39",
		},
		{
			name:      "the worktrees trail runs to the hovered worktree",
			press:     []string{"shift+tab", "G"},
			wantTrail: "worktrees › task/agent39",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, paneltest.Params{})

			harness.Press(tt.press...)

			assert.Equal(t, tt.wantTrail, paneltest.Trail(harness.Frame()))
		})
	}

	t.Run("an empty view's trail is the view alone", func(t *testing.T) {
		harness := paneltest.OpenAgents(t)

		assert.Equal(t, "agents", paneltest.Trail(harness.Frame()))
	})

	t.Run("the trail follows the cursor", func(t *testing.T) {
		harness := openCrowded(t, paneltest.Params{})

		harness.Press("G")
		require.Equal(t, "agents › agent39", paneltest.Trail(harness.Frame()))

		harness.Press("k")
		assert.Equal(t, "agents › agent38", paneltest.Trail(harness.Frame()))
	})
}
