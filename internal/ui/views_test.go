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

		harness.press(append(widenListPresses, "tab", "r")...)

		lines := splitLines(harness.readFrame())
		require.GreaterOrEqual(t, len(lines), 2)
		logo, _, _ := strings.Cut(lines[0], "│")
		assert.True(t, strings.HasPrefix(strings.TrimSpace(logo), "\U000F050C komrad"))
		assert.True(t, strings.HasSuffix(strings.TrimSpace(logo), "\uf444 local"))
		views, _, _ := strings.Cut(lines[1], "│")
		assert.Equal(t, "agents  query  schedules  archive  worktrees", strings.TrimSpace(views))
		lit := paint.Panel.Style(paint.ToneAccent)
		assert.Contains(t, harness.readRawFrame(), lit.Render("a")+lit.Underline(true).Render("r")+lit.Render("chive"), "the view that is up is lit with its jump key underlined")
	})

	t.Run("a page opened from a view draws the trail back to it under the switcher", func(t *testing.T) {
		harness, _ := openDocuments(t)

		lines := splitLines(harness.readFrame())
		require.GreaterOrEqual(t, len(lines), 3)
		trail, _, _ := strings.Cut(lines[2], "│")
		assert.True(t, strings.HasPrefix(trail, "agents › author › documents"))
		lit := paint.Panel.Style(paint.ToneAccent)
		assert.Contains(t, harness.readRawFrame(), lit.Underline(true).Render("a")+lit.Render("gents"), "the view it was opened from is lit")
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
			name:       "a letter jumps to its view",
			press:      []string{"tab", "s"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "tab opens the next view",
			press:      []string{"tab", "tab"},
			wantScreen: searchScreen,
		},
		{
			name:       "the switcher keeps the keys through each view it opens",
			press:      []string{"tab", "tab", "tab"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "shift-tab wraps to the last view",
			press:      []string{"tab", "shift+tab"},
			wantScreen: worktreesScreen,
		},
		{
			name:       "escape hands the keys back to the view it stands on",
			press:      []string{"tab", "tab", "esc", "x"},
			wantScreen: searchScreen,
		},
		{
			name:       "a letter closes the switcher behind it",
			press:      []string{"tab", "r", "tab"},
			wantScreen: archiveScreen,
		},
		{
			name:       "the switcher answers from a view it opened",
			press:      []string{"tab", "r", "tab", "a"},
			wantScreen: agentsScreen,
		},
		{
			name:       "escape on the agents opens the switcher",
			press:      []string{"esc", "s"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "escape on the schedules opens the switcher",
			press:      []string{"tab", "s", "esc", "r"},
			wantScreen: archiveScreen,
		},
		{
			name:       "escape on the archive opens the switcher",
			press:      []string{"tab", "r", "esc", "w"},
			wantScreen: worktreesScreen,
		},
		{
			name:       "escape on the worktrees opens the switcher",
			press:      []string{"tab", "w", "esc", "s"},
			wantScreen: schedulesScreen,
		},
		{
			name:       "escape in the query line opens the switcher",
			press:      []string{"tab", "q", "esc", "r"},
			wantScreen: archiveScreen,
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

		harness.press(append(widenListPresses, "tab")...)
		assert.Empty(t, readHoveredRow(harness.readFrame()))

		harness.press("esc")
		assert.NotEmpty(t, readHoveredRow(harness.readFrame()), "and the list has the keys again")
	})

	t.Run("the legend names the switcher's keys while it is open", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{})

		harness.press(append(widenListPresses, "tab")...)

		assert.Contains(t, readLegend(harness.readFrame()), "a agents • r archive • esc done")
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
			press:     []string{"tab", "q"},
			wantTrail: "query",
		},
		{
			name:      "the schedules trail runs to the hovered schedule",
			press:     []string{"tab", "s", "G"},
			wantTrail: "schedules › agent39",
		},
		{
			name:      "the archive trail runs to the hovered agent",
			press:     []string{"tab", "r", "G"},
			wantTrail: "archive › agent39",
		},
		{
			name:      "the worktrees trail runs to the hovered worktree",
			press:     []string{"tab", "w", "G"},
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

		harness.press("tab", "s", "G")

		muted := paint.Panel.Style(paint.ToneMuted)
		assert.Contains(t, harness.readRawFrame(), muted.Render("schedules › agent39"))
	})
}
