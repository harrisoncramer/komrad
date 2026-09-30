package ui_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"komrad/internal/ui"
	"komrad/lib/xptr"
	agentv1 "komrad/proto/gen/agent/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const stripLegend = "esc back to the list"

// TestTabStrip covers the sort strip every tabbed list draws under its filter line.
func TestTabStrip(t *testing.T) {
	tests := []struct {
		name   string
		press  []string
		screen string
		second string
		kinds  string
	}{
		{
			name:   "the agents tree",
			screen: agentsScreen,
			second: "activity",
		},
		{
			name:   "the archive",
			press:  []string{"shift+tab", "shift+tab"},
			screen: archiveScreen,
			second: "name",
			kinds:  "F   agents  schedules",
		},
		{
			name:   "the worktrees",
			press:  []string{"shift+tab"},
			screen: worktreesScreen,
			second: "last-accessed",
			kinds:  "F   all  inactive  merged",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{})
			harness.press(tt.press...)

			sidebar := readSidebar(harness.readFrame())
			strip := slices.IndexFunc(sidebar, func(line string) bool { return strings.HasPrefix(line, "f ") })
			require.GreaterOrEqual(t, strip, 0)
			require.Greater(t, len(sidebar), strip+3)
			gap := strip + 1
			if tt.kinds != "" {
				assert.Equal(t, tt.kinds, sidebar[gap], "the row of kinds stands under the strip")
				gap++
			}
			assert.True(t, strings.HasPrefix(sidebar[gap], "/"), "the search stands under the strips")
			gap++
			assert.Empty(t, sidebar[gap], "one blank row stands before the list")

			harness.press("f")
			frame := harness.readFrame()
			require.Contains(t, readLegend(frame), stripLegend)
			assert.Empty(t, readHoveredRow(frame), "the row's arrow stands down")

			harness.press("tab", "j", "k")
			assert.Contains(t, readSidebar(harness.readFrame())[strip], "↑ "+tt.second)

			harness.press("esc")
			frame = harness.readFrame()
			assert.Equal(t, tt.screen, harness.readScreen(), "the press is spent on the strip rather than on the screen's back key")
			assert.Contains(t, readSidebar(frame)[strip], "↑ "+tt.second, "the choice is kept")
			assert.NotEmpty(t, readHoveredRow(frame), "and the list has the keys again")
		})
	}

	t.Run("the record and the strip take the keys from each other", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)

		harness.press("pick:record", "f")
		frame := harness.readFrame()
		assert.Contains(t, readLegend(frame), stripLegend)
		assert.Empty(t, readRecordCursor(frame))

		harness.press("esc", "pick:record")
		assert.NotEmpty(t, readRecordCursor(harness.readFrame()))
	})

	t.Run("the legend names the strip's own keys while it holds them", func(t *testing.T) {
		harness := openWorktrees(t)
		harness.press("?")
		assert.Regexp(t, `(?m)^f +sort`, readLegend(harness.readFrame()))

		harness.press("f")
		legend := readLegend(harness.readFrame())
		assert.Contains(t, legend, "tab/S-tab")
		assert.Contains(t, legend, "sort down/up, flip")
		assert.Contains(t, legend, "back to the list")
		assert.NotContains(t, legend, "jump to agent")
	})

	t.Run("a pane too short for the strip and a row draws no strip and takes no keys", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{
			width:  80,
			height: 6,
		})
		harness.press("shift+tab", "G")
		frame := harness.readFrame()
		require.NotContains(t, readSidebar(frame)[1], "f ", "the strip gives up its row")
		assert.Contains(t, frame, "/trees/agent39", "to the row the cursor stands on")

		harness.press("f")
		assert.NotContains(t, readLastLine(harness.readFrame()), stripLegend)

		resize(harness, 80, 7)
		assert.NotContains(t, readLastLine(harness.readFrame()), stripLegend, "and the refused press does not wake once the pane grows")
	})

	t.Run("a pane one row taller draws the strip and still the cursor's row", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{
			width:  80,
			height: 7,
		})
		harness.press("shift+tab", "G")

		frame := harness.readFrame()
		assert.Contains(t, readSidebar(frame)[1], "f ")
		assert.Contains(t, frame, "/trees/agent39")
		assert.Len(t, splitLines(frame), 7)
	})

	t.Run("a shrink past the strip gives the keys back to the list for good", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{
			width:  80,
			height: 7,
		})
		harness.press("shift+tab", "G", "f")
		require.Contains(t, readLastLine(harness.readFrame()), stripLegend)

		resize(harness, 80, 6)
		assert.NotContains(t, readLastLine(harness.readFrame()), stripLegend)

		resize(harness, 80, 7)
		harness.press("k")
		assert.Contains(t, harness.readFrame(), "/trees/agent38", "the next press moves the list")
	})
}

// TestTabStripGolden draws each tabbed sidebar whole, grouped and flat.
func TestTabStripGolden(t *testing.T) {
	tests := []struct {
		name  string
		rows  []proto.Message
		press []string
	}{
		{
			name:  "agents-sort-strip",
			rows:  buildPinnedArchive(),
			press: []string{"j"},
		},
		{
			name:  "agents-sort-strip-flat",
			rows:  buildPinnedArchive(),
			press: []string{"j", "f", "tab", "j", "shift+tab", "shift+tab", "j", "esc"},
		},
		{
			name:  "archive-sort-strip",
			rows:  buildPinnedArchive(),
			press: []string{"shift+tab", "shift+tab", "f", "shift+tab", "j", "esc", "k"},
		},
		{
			name:  "archive-sort-strip-flat",
			rows:  buildPinnedArchive(),
			press: []string{"shift+tab", "shift+tab", "esc"},
		},
		{
			name:  "worktrees-sort-strip",
			rows:  buildMeasuredWorktrees(),
			press: []string{"shift+tab", "esc", "f", "shift+tab", "j", "esc"},
		},
		{
			name:  "worktrees-sort-strip-flat",
			rows:  buildMeasuredWorktrees(),
			press: []string{"shift+tab", "esc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := newPanelHarness(t, panelHarnessParams{
				width:  44,
				height: 28,
			})
			answerWorktreeReads(harness)
			harness.watch(buildSnapshot(tt.rows...))

			harness.press(tt.press...)

			ui.RequireGolden(t, tt.name, harness.readRawFrame())
		})
	}
}

func buildPinnedArchive() []proto.Message {
	archivedAt := time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local).Format(time.RFC3339)

	rows := buildWorktrees()
	for _, row := range rows {
		if agent, isAgent := row.(*agentv1.Agent); isAgent && agent.ArchivedAt != nil {
			agent.ArchivedAt = xptr.Ptr(archivedAt)
		}
	}

	return rows
}

func readLastLine(frame string) string {
	lines := splitLines(frame)

	return lines[len(lines)-1]
}
