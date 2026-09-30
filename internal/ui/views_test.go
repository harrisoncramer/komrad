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

		harness.press(append(widenListPresses, "shift+tab", "shift+tab", "esc")...)

		lines := splitLines(harness.readFrame())
		require.GreaterOrEqual(t, len(lines), 4)
		logo, _, _ := strings.Cut(lines[0], "│")
		assert.True(t, strings.HasPrefix(strings.TrimSpace(logo), "\U000F050C komrad › archive"))
		engine, _, _ := strings.Cut(lines[1], "│")
		assert.Equal(t, "\uf444 local", strings.TrimSpace(engine), "the engine stands under the logo")
		blank, _, _ := strings.Cut(lines[2], "│")
		assert.Empty(t, strings.TrimSpace(blank), "a blank row stands under the engine")
		views, _, _ := strings.Cut(lines[3], "│")
		assert.Equal(t, "agents  query  schedules  archive  worktrees", strings.TrimSpace(views))
		lit := paint.Panel.Style(paint.TonePrimary)
		assert.Contains(t, harness.readRawFrame(), lit.Render("archive"), "the view that is up is lit")
	})

	t.Run("a page opened from a view draws the trail back to it beside the logo", func(t *testing.T) {
		harness, _ := openDocuments(t)

		assert.True(t, strings.HasPrefix(readTrail(harness.readFrame()), "agents › author › documents"))
		lit := paint.Panel.Style(paint.TonePrimary)
		assert.Contains(t, harness.readRawFrame(), lit.Render("agents"), "the view it was opened from is lit")
	})

	t.Run("a pane too short for the switcher still draws the trail", func(t *testing.T) {
		harness, _ := openDocuments(t)

		resize(harness, 80, 16)
		frame := harness.readFrame()
		assert.NotContains(t, frame, "komrad")
		assert.Contains(t, frame, "agents › author › documents")
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
			name:       "tab opens the next view",
			press:      []string{"tab"},
			wantScreen: searchScreen,
		},
		{
			name:       "shift-tab opens the previous view, wrapping to the last",
			press:      []string{"shift+tab"},
			wantScreen: worktreesScreen,
		},
		{
			name:       "the switcher keeps tab through each view it opens",
			press:      []string{"tab", "tab"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "the switcher keeps shift-tab through each view it opens",
			press:      []string{"shift+tab", "shift+tab"},
			wantScreen: archiveScreen,
		},
		{
			name:       "escape hands the keys back to the view it stands on",
			press:      []string{"tab", "esc", "x"},
			wantScreen: searchScreen,
		},
		{
			name:       "escape on the agents opens the switcher without moving",
			press:      []string{"esc"},
			wantScreen: agentsScreen,
		},
		{
			name:       "escape on the agents hands tab to the switcher",
			press:      []string{"esc", "tab", "tab"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "escape on the schedules opens the switcher",
			press:      []string{"tab", "tab", "esc", "esc", "tab"},
			wantScreen: archiveScreen,
		},
		{
			name:       "escape on the archive opens the switcher",
			press:      []string{"shift+tab", "shift+tab", "esc", "esc", "tab"},
			wantScreen: worktreesScreen,
		},
		{
			name:       "escape on the worktrees opens the switcher",
			press:      []string{"shift+tab", "esc", "esc", "tab"},
			wantScreen: agentsScreen,
		},
		{
			name:       "escape in the query line opens the switcher",
			press:      []string{"tab", "esc", "esc", "tab"},
			wantScreen: schedulesScreen,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{})

			harness.press(tt.press...)

			assert.Equal(t, tt.wantScreen, harness.readScreen())
		})
	}

	t.Run("the list's arrow stands down while the switcher is open", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{})

		harness.press(append(widenListPresses, "esc")...)
		assert.Empty(t, readHoveredRow(harness.readFrame()))

		harness.press("esc")
		assert.NotEmpty(t, readHoveredRow(harness.readFrame()), "and the list has the keys again")
	})

	t.Run("a key the switcher has no use for closes it and reaches the view", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{})

		harness.press(append(widenListPresses, "tab", "tab", "G")...)

		frame := harness.readFrame()
		assert.Equal(t, schedulesScreen, harness.readScreen())
		assert.NotEmpty(t, readHoveredRow(frame), "the switcher let the list have the keys")
		assert.Equal(t, "schedules › agent39", readTrail(frame), "and the list took the key")
	})

	t.Run("the legend names the switcher's keys while it is open", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{})

		harness.press(append(widenListPresses, "esc")...)

		assert.Contains(t, readLegend(harness.readFrame()), "esc done • tab/S-tab switch")
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
			name:      "the query trail names the view alone before a search",
			press:     []string{"tab"},
			wantTrail: "query",
		},
		{
			name:      "the schedules trail runs to the hovered schedule",
			press:     []string{"tab", "tab", "G"},
			wantTrail: "schedules › agent39",
		},
		{
			name:      "the archive trail runs to the hovered agent",
			press:     []string{"shift+tab", "shift+tab", "G"},
			wantTrail: "archive › agent39",
		},
		{
			name:      "the worktrees trail runs to the hovered worktree",
			press:     []string{"shift+tab", "G"},
			wantTrail: "worktrees › agent/agent39",
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

	t.Run("a pane too short for the switcher leaves the hovered row off", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{height: 16})

		assert.NotContains(t, harness.readFrame(), "agents ›")
	})

	t.Run("the trail is drawn in one muted tone", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{})

		harness.press("tab", "tab", "G")

		muted := paint.Panel.Style(paint.ToneMuted)
		assert.Contains(t, harness.readRawFrame(), muted.Render("schedules › agent39"))
	})
}
