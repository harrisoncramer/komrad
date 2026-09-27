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

// TestSelection covers the bar a chosen row is drawn under.
func TestSelection(t *testing.T) {
	bar := paint.BackgroundSequence(paint.SelectionBackground)

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
			name: "the archive",
			rows: []proto.Message{
				newLiveWorkflow("w1", "backend"),
				newArchivedAgentAt("auth", "2026-07-30T12:00:00Z"),
				newArchivedAgentAt("api", "2026-07-30T11:00:00Z"),
				newArchivedAgentAt("db", "2026-07-30T10:00:00Z"),
			},
			press: []string{"O", "z", "j"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openAgents(t, tt.rows...)
			harness.press(tt.press...)
			harness.press("j")
			require.NotContains(t, findDrawnRow(t, harness, "api"), bar)

			harness.press("v", "j")

			assert.Contains(t, findDrawnRow(t, harness, "api"), bar)
			assert.NotContains(t, findDrawnRow(t, harness, "db"), bar, "and only that row")
		})
	}

	t.Run("the bar stays while a confirm has the keys", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		harness.press("j", "v")

		harness.press("z")
		require.Contains(t, harness.readFrame(), `Archive agent "api"?`)

		assert.Contains(t, findDrawnRow(t, harness, "api"), bar)
	})
}

func findDrawnRow(t *testing.T, harness *panelHarness, name string) string {
	t.Helper()

	for _, line := range splitLines(harness.readRawFrame()) {
		row, isRow := cutListRow(ansi.Strip(line))
		if isRow && strings.TrimSpace(row) == name {
			return line
		}
	}

	require.Failf(t, "no row is drawn for the name", "%q", name)

	return ""
}
