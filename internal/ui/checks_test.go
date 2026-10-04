package ui_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const checksScreen = "the checks"

// TestChecksOpen covers the screen opening from the subpages menu.
func TestChecksOpen(t *testing.T) {
	t.Parallel()

	t.Run("the menu opens the agent's checks on an empty list", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSubpagesAgents()...)
		moveToAgentRow(t, harness, "author")
		harness.press("space", "x")

		frame := harness.readFrame()
		assert.Equal(t, checksScreen, harness.readScreen())
		assert.Contains(t, frame, "agents › author › checks")
		assert.Contains(t, frame, "no checks yet")
	})

	t.Run("back returns to the list on the agent it opened for", func(t *testing.T) {
		t.Parallel()

		harness := openAgents(t, buildSubpagesAgents()...)
		moveToAgentRow(t, harness, "author")
		harness.press("space", "x", "esc")

		assert.Equal(t, agentsScreen, harness.readScreen())
		assert.Contains(t, readHoveredRow(harness.readFrame()), "author")
	})
}
