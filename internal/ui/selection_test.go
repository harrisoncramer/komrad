package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/ui/paint"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// TestSelection covers the bar a chosen row is rendered under.
func TestSelection(t *testing.T) {
	bar := paint.BackgroundSequence(paint.Panel.Background(paint.ShadeSelection))

	tests := []struct {
		name  string
		rows  []proto.Message
		press []string
	}{
		{
			name: "the agents tree",
			rows: buildSelectable(),
		},
		{
			name: "the archived agents",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newArchivedAgentAt("auth", "2026-07-30T12:00:00Z"),
				newArchivedAgentAt("api", "2026-07-30T11:00:00Z"),
				newArchivedAgentAt("db", "2026-07-30T10:00:00Z"),
			},
			press: []string{"F", "tab", "esc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, tt.rows...)
			harness.press(tt.press...)
			harness.press("j")
			require.NotContains(t, findRenderedRow(t, harness, "api"), bar)

			harness.press("v", "j")

			assert.Contains(t, findRenderedRow(t, harness, "api"), bar)
			assert.NotContains(t, findRenderedRow(t, harness, "db"), bar, "and only that row")
		})
	}

	t.Run("the bar stays while a menu has the keys", func(t *testing.T) {
		harness := openAgents(t, withTasks(buildSelectable())...)
		harness.press("j", "j", "v")

		harness.press("space", "a")
		require.Contains(t, harness.readFrame(), "archive all")

		assert.Contains(t, findRenderedRow(t, harness, "api"), bar)
	})

	t.Run("a task's row is under the bar while every agent on it is", func(t *testing.T) {
		harness := openAgents(t, buildTaskFamily()...)

		harness.press("j", "j", "j", "v", "j", "v")
		require.NotContains(t, findRenderedRow(t, harness, "task-author"), bar, "two of its three agents")

		harness.press("k", "k", "v")
		require.Contains(t, findRenderedRow(t, harness, "task-author"), bar, "all three")

		harness.press("j", "v")
		assert.NotContains(t, findRenderedRow(t, harness, "task-author"), bar, "one dropped again")
	})
}

func findRenderedRow(t *testing.T, harness *panelHarness, name string) string {
	t.Helper()

	lines := findRenderedRows(t, harness, name)
	require.NotEmptyf(t, lines, "no row is rendered for %q", name)

	return lines[0]
}

func findRenderedRows(t *testing.T, harness *panelHarness, name string) []string {
	t.Helper()

	raw := harness.readRawFrame()
	lines := splitLines(raw)
	var found []string
	for _, line := range readListLines(ansi.Strip(raw)) {
		if strings.TrimLeft(line.row, "│├└─ ") == name {
			found = append(found, lines[line.index])
		}
	}

	return found
}
