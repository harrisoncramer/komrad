package ui_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/ui"
	agentv1 "weave/proto/gen/agent/v1"

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
		title  string
		second string
	}{
		{
			name:   "the agents tree",
			title:  agentsTitle,
			second: "activity",
		},
		{
			name:   "the archive",
			press:  []string{"Z"},
			title:  archiveTitle,
			second: "name",
		},
		{
			name:   "the worktrees",
			press:  []string{"t"},
			title:  worktreesTitle,
			second: "last-accessed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{})
			harness.press(tt.press...)

			sidebar := readSidebar(harness.readFrame())
			assert.Contains(t, sidebar[2], "/")
			assert.Contains(t, sidebar[3], "f")
			assert.Empty(t, sidebar[4], "one blank row stands before the list")

			harness.press("f")
			frame := harness.readFrame()
			require.Contains(t, readLegend(frame), stripLegend)
			assert.Empty(t, readHoveredRow(frame), "the row's arrow stands down")

			harness.press("tab", "j", "k")
			assert.Contains(t, readSidebar(harness.readFrame())[3], "↑ "+tt.second)

			harness.press("esc")
			frame = harness.readFrame()
			assert.Contains(t, splitLines(frame)[0], tt.title, "the press is spent on the strip rather than on the screen's back key")
			assert.Contains(t, readSidebar(frame)[3], "↑ "+tt.second, "the choice is kept")
			assert.NotEmpty(t, readHoveredRow(frame), "and the list has the keys again")
		})
	}

	t.Run("the record and the strip take the keys from each other", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)

		harness.press("ctrl+t", "f")
		frame := harness.readFrame()
		assert.Contains(t, readLegend(frame), stripLegend)
		assert.Empty(t, readRecordCursor(frame))

		harness.press("esc", "ctrl+t")
		frame = harness.readFrame()
		assert.NotContains(t, readLegend(frame), stripLegend)
		assert.NotEmpty(t, readRecordCursor(frame))
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
		harness.press("t", "G")
		frame := harness.readFrame()
		require.NotContains(t, readSidebar(frame)[3], "f ", "the strip gives up its row")
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
		harness.press("t", "G")

		frame := harness.readFrame()
		assert.Contains(t, readSidebar(frame)[3], "f ")
		assert.Contains(t, frame, "/trees/agent39")
		assert.Len(t, splitLines(frame), 7)
	})

	t.Run("a shrink past the strip gives the keys back to the list for good", func(t *testing.T) {
		harness := openCrowded(t, panelHarnessParams{
			width:  80,
			height: 7,
		})
		harness.press("t", "G", "f")
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
			press: []string{"Z"},
		},
		{
			name:  "archive-sort-strip-flat",
			rows:  buildPinnedArchive(),
			press: []string{"Z", "f", "shift+tab", "j", "esc"},
		},
		{
			name:  "worktrees-sort-strip",
			rows:  buildMeasuredWorktrees(),
			press: []string{"t"},
		},
		{
			name:  "worktrees-sort-strip-flat",
			rows:  buildMeasuredWorktrees(),
			press: []string{"t", "f", "shift+tab", "j", "esc"},
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

			requireFrameGolden(t, tt.name, harness.panel.View())
		})
	}
}

func buildPinnedArchive() []proto.Message {
	archivedAt := time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local).Format(time.RFC3339)

	rows := buildWorktrees()
	for _, row := range rows {
		if agent, isAgent := row.(*agentv1.Agent); isAgent && agent.ArchivedAt != nil {
			agent.ArchivedAt = proto.String(archivedAt)
		}
	}

	return rows
}

func readLastLine(frame string) string {
	lines := splitLines(frame)

	return lines[len(lines)-1]
}

func requireFrameGolden(t *testing.T, name, got string) {
	t.Helper()

	path := filepath.Join("testdata", name+".golden")
	if ui.UpdateGoldens() {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))

		return
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err, "no golden file yet, so run the suite with -update")
	assert.Equal(t, string(want), got)
}
