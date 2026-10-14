package ui_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViews covers the switcher rendered over every list and the keys that move between views.
func TestViews(t *testing.T) {
	t.Run("a page opened from a view renders the trail back to it beside the logo", func(t *testing.T) {
		harness := openLogs(t, tailOfTwo())

		assert.True(t, strings.HasPrefix(readTrail(harness.readFrame()), "agents › reviewer › logs"))
	})

	tests := []struct {
		name       string
		press      []string
		wantScreen string
	}{
		{
			name:       "tab opens the next view",
			press:      []string{"tab"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "shift-tab opens the previous view, wrapping to the last",
			press:      []string{"shift+tab"},
			wantScreen: worktreesScreen,
		},
		{
			name:       "the switcher keeps tab through each view it opens",
			press:      []string{"tab", "tab"},
			wantScreen: documentsScreen,
		},
		{
			name:       "the switcher keeps shift-tab through each view it opens",
			press:      []string{"shift+tab", "shift+tab"},
			wantScreen: documentsScreen,
		},
		{
			name:       "escape hands the keys back to the view it stands on",
			press:      []string{"tab", "esc", "x"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "escape on the agents opens the switcher without moving",
			press:      []string{"esc"},
			wantScreen: agentsScreen,
		},
		{
			name:       "escape on the agents hands tab to the switcher",
			press:      []string{"esc", "tab", "tab"},
			wantScreen: documentsScreen,
		},
		{
			name:       "escape on the schedules opens the switcher",
			press:      []string{"tab", "esc", "esc", "tab"},
			wantScreen: documentsScreen,
		},
		{
			name:       "escape on the documents opens the switcher",
			press:      []string{"tab", "tab", "esc", "esc", "tab"},
			wantScreen: worktreesScreen,
		},
		{
			name:       "escape on the worktrees opens the switcher",
			press:      []string{"shift+tab", "esc", "esc", "tab"},
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

	t.Run("the list keeps its arrow while the switcher is open", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{})
		harness.press(widenListPresses...)
		hovered := readHoveredRow(harness.readFrame())
		require.NotEmpty(t, hovered)

		harness.press("esc")
		require.Contains(t, harness.readFrame(), "tab/S-tab switch")
		assert.Equal(t, hovered, readHoveredRow(harness.readFrame()))
	})

	t.Run("a key the switcher has no use for closes it and reaches the view", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{})

		harness.press(append(widenListPresses, "tab", "G")...)

		frame := harness.readFrame()
		assert.Equal(t, schedulesScreen, harness.readScreen())
		assert.NotEmpty(t, readHoveredRow(frame), "the switcher let the list have the keys")
		assert.Equal(t, "schedules › agent00", readTrail(frame), "and the list took the key")
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
			harness := openCrowded(t, panelHarnessParams{})

			harness.press(tt.press...)

			assert.Equal(t, tt.wantTrail, readTrail(harness.readFrame()))
		})
	}

	t.Run("an empty view's trail is the view alone", func(t *testing.T) {
		harness := openAgents(t)

		assert.Equal(t, "agents", readTrail(harness.readFrame()))
	})

	t.Run("the trail follows the cursor", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{})

		harness.press("G")
		require.Equal(t, "agents › agent39", readTrail(harness.readFrame()))

		harness.press("k")
		assert.Equal(t, "agents › agent38", readTrail(harness.readFrame()))
	})
}
