package ui_test

import (
	"strings"
	"testing"

	"komrad/internal/ui/paneltest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListPageKeys covers a page key moving the cursor ten rows and stopping at either end.
func TestListPageKeys(t *testing.T) {
	tests := []struct {
		name  string
		press []string
		want  string
	}{
		{
			name:  "a page down is ten rows",
			press: []string{"ctrl+d"},
			want:  "├─ agent10",
		},
		{
			name:  "a page past the top lands on the first row",
			press: []string{"ctrl+d", "ctrl+u", "ctrl+u"},
			want:  "everything",
		},
		{
			name:  "a page past the bottom lands on the last row",
			press: []string{"G", "k", "ctrl+d"},
			want:  "└─ agent39",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, paneltest.Params{})

			harness.Press(tt.press...)

			assert.True(t, strings.HasPrefix(paneltest.HoveredRow(harness.Frame()), tt.want))
		})
	}
}

// TestListScrollKeys covers the keys the mouse wheel presses moving every list one row.
func TestListScrollKeys(t *testing.T) {
	tests := []struct {
		name   string
		screen []string
		first  string
		second string
	}{
		{
			name:   "the agents tree",
			first:  "everything",
			second: "├─ agent00",
		},
		{
			name:   "the archived agents",
			screen: []string{"F", "tab", "esc"},
			first:  "everything",
			second: "├─ agent",
		},
		{
			name:   "the schedules",
			screen: []string{"tab"},
			first:  "agent39",
			second: "agent38",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, paneltest.Params{})
			harness.Press(tt.screen...)
			harness.Press("g", "g")
			require.Contains(t, paneltest.HoveredRow(harness.Frame()), tt.first)

			harness.Press("ctrl+n")
			assert.Contains(t, paneltest.HoveredRow(harness.Frame()), tt.second, "one turn down is one row")

			harness.Press("ctrl+p")
			assert.Contains(t, paneltest.HoveredRow(harness.Frame()), tt.first, "and one turn up is back")

			harness.Press("ctrl+p")
			assert.Contains(t, paneltest.HoveredRow(harness.Frame()), tt.first, "the top stops it")
		})
	}

	t.Run("a turn up on the record's first field keeps the record", func(t *testing.T) {
		harness := paneltest.OpenAgents(t, buildSelectable()...)
		harness.Press("pick:record")

		harness.Press("ctrl+n")
		assert.Equal(t, "flags      —", paneltest.RecordCursor(harness.Frame()), "a turn down is a field")

		harness.Press("ctrl+p")
		assert.Equal(t, "agent      —", paneltest.RecordCursor(harness.Frame()))

		harness.Press("ctrl+p")
		assert.Equal(t, "agent      —", paneltest.RecordCursor(harness.Frame()), "and a turn up past it keeps the record too")
	})
}
