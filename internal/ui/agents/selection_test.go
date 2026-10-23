package agents_test

import (
	"strings"
	"testing"
	"time"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paneltest"

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
				paneltest.Workflow("w1", "backend"),
				newArchivedAgentAt("auth", time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)),
				newArchivedAgentAt("api", time.Date(2026, 7, 30, 11, 0, 0, 0, time.UTC)),
				newArchivedAgentAt("db", time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC)),
			},
			press: []string{"F", "tab", "esc", "j"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := paneltest.OpenAgents(t, tt.rows...)
			harness.Press(tt.press...)
			harness.Press("j")
			require.NotContains(t, findRenderedRow(t, harness, "api"), bar)

			harness.Press("v", "j")

			assert.Contains(t, findRenderedRow(t, harness, "api"), bar)
			assert.NotContains(t, findRenderedRow(t, harness, "db"), bar, "and only that row")
		})
	}

	t.Run("the bar stays while a menu has the keys", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, withTasks(buildSelectable())...)
		harness.Press("j", "v")

		harness.Press("space", "a")
		require.Contains(t, harness.Frame(), "archive all")

		assert.Contains(t, findRenderedRow(t, harness, "task-api"), bar)
	})

	t.Run("a selection survives a search", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, withTasks(buildSelectable())...)

		harness.Press("v", "j", "j", "v", "/", "d", "b", "enter", "space", "a")

		assert.Contains(t, harness.Frame(), "2 agents › archive")
	})

	t.Run("a task's row is under the bar while its writer is", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildReviewedTasks()...)

		harness.Press("j", "j", "v", "j", "v")
		require.Contains(t, findRenderedRow(t, harness, "second-reviewer"), bar)
		require.Contains(t, findRenderedRow(t, harness, "reviewer"), bar)
		require.NotContains(t, findRenderedRow(t, harness, "task-writer"), bar, "its reviewers alone")

		harness.Press("k", "k", "v")
		require.Contains(t, findRenderedRow(t, harness, "task-writer"), bar, "its writer too")

		harness.Press("v")
		assert.NotContains(t, findRenderedRow(t, harness, "task-writer"), bar, "its writer dropped again")
	})
}

func findRenderedRow(t *testing.T, harness *paneltest.Panel, name string) string {
	t.Helper()

	lines := findRenderedRows(t, harness, name)
	require.NotEmptyf(t, lines, "no row is rendered for %q", name)

	return lines[0]
}

func findRenderedRows(t *testing.T, harness *paneltest.Panel, name string) []string {
	t.Helper()

	raw := harness.RawFrame()
	lines := paneltest.SplitLines(raw)
	var found []string
	for _, line := range paneltest.ListLines(ansi.Strip(raw)) {
		if strings.TrimPrefix(strings.TrimLeft(line.Row, "│├└─ "), reviewerMark) == name {
			found = append(found, lines[line.Index])
		}
	}

	return found
}
