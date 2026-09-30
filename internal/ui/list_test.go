package ui_test

import (
	"strings"
	"testing"

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
			want:  "  agent10",
		},
		{
			name:  "a page past the top lands on the first row",
			press: []string{"ctrl+d", "ctrl+u", "ctrl+u"},
			want:  "everything",
		},
		{
			name:  "a page past the bottom lands on the last row",
			press: []string{"G", "k", "ctrl+d"},
			want:  "  agent39",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{})

			harness.press(tt.press...)

			assert.True(t, strings.HasPrefix(readHoveredRow(harness.readFrame()), tt.want))
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
			second: "  agent00",
		},
		{
			name:   "the archive",
			screen: []string{"shift+tab", "shift+tab"},
			first:  "agent00",
			second: "agent01",
		},
		{
			name:   "the schedules",
			screen: []string{"tab", "tab"},
			first:  "agent39",
			second: "agent38",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			harness := openCrowded(t, panelHarnessParams{})
			harness.press(tt.screen...)
			harness.press("g", "g")
			require.Contains(t, readHoveredRow(harness.readFrame()), tt.first)

			harness.press("ctrl+n")
			assert.Contains(t, readHoveredRow(harness.readFrame()), tt.second, "one turn down is one row")

			harness.press("ctrl+p")
			assert.Contains(t, readHoveredRow(harness.readFrame()), tt.first, "and one turn up is back")

			harness.press("ctrl+p")
			assert.Contains(t, readHoveredRow(harness.readFrame()), tt.first, "the top stops it")
		})
	}

	t.Run("a turn up on the record's first field keeps the record", func(t *testing.T) {
		harness := openAgents(t, buildSelectable()...)
		harness.press("pick:record")

		harness.press("ctrl+n")
		assert.Equal(t, "agent      —", readRecordCursor(harness.readFrame()), "a turn down is a field")

		harness.press("ctrl+p")
		assert.Equal(t, "name       auth", readRecordCursor(harness.readFrame()))

		harness.press("ctrl+p")
		assert.Equal(t, "name       auth", readRecordCursor(harness.readFrame()), "and a turn up past it keeps the record too")
	})
}
