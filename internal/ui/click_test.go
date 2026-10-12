package ui_test

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClickRow covers a left click putting the cursor on the list row under it.
func TestClickRow(t *testing.T) {
	t.Run("a click on a row puts the cursor on it", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		require.Equal(t, "└─ auth", readHoveredRow(harness.readFrame()))

		harness.send(clickOn(t, harness.readFrame(), "backend"))

		assert.Equal(t, "backend", readHoveredRow(harness.readFrame()))
	})

	t.Run("a click on a row hands the keys back from the record", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		harness.press("pick:record")

		harness.send(clickOn(t, harness.readFrame(), "backend"))

		frame := harness.readFrame()
		assert.Empty(t, readRecordCursor(frame))
		assert.Equal(t, "backend", readHoveredRow(frame))
	})

	t.Run("a click on a row hands the keys back from the switcher", func(t *testing.T) {
		harness := openAgents(t, buildWorktrees()...)
		harness.press("shift+tab")
		require.Empty(t, readHoveredRow(harness.readFrame()))

		harness.send(clickOn(t, harness.readFrame(), "reviewer"))

		assert.Contains(t, readHoveredRow(harness.readFrame()), "reviewer")
	})

	t.Run("a click on a row hands the keys back from the tabs", func(t *testing.T) {
		harness := openWorktrees(t)
		harness.press("f")
		require.Empty(t, readHoveredRow(harness.readFrame()))

		harness.send(clickOn(t, harness.readFrame(), "reviewer"))

		assert.Contains(t, readHoveredRow(harness.readFrame()), "reviewer")
	})

	t.Run("a right click leaves the cursor where it was", func(t *testing.T) {
		harness := openAgents(t, buildRecorded()...)
		click := clickOn(t, harness.readFrame(), "backend")
		click.Button = tea.MouseRight

		harness.send(click)

		assert.Equal(t, "└─ auth", readHoveredRow(harness.readFrame()))
	})
}

func clickOn(t *testing.T, frame, row string) tea.MouseClickMsg {
	t.Helper()

	for y, line := range splitLines(frame) {
		sidebar, _, _ := strings.Cut(line, "│")
		if slices.Contains(strings.Fields(sidebar), row) {
			return tea.MouseClickMsg{X: 2, Y: y, Button: tea.MouseLeft}
		}
	}

	require.Failf(t, "no row in the frame", "%q", row)

	return tea.MouseClickMsg{}
}
