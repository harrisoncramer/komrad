package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestPageKeysMoveTheCursorByTheStep covers a page landing on the end rather than refusing to
// move past it.
func TestPageKeysMoveTheCursorByTheStep(t *testing.T) {
	m := screensModel()
	m.height = 29
	m.screen = screenAgents
	m.agents.cursor = 0

	press := func(pressed string) {
		next, _ := m.route(keyMsgOf(t, pressed))
		m = next.(model)
	}

	press("ctrl+d")
	assert.Equal(t, navPageStep, m.agents.cursor)

	press("ctrl+u")
	press("ctrl+u")
	assert.Equal(t, 0, m.agents.cursor, "a page past the top lands on the first row")

	last := len(m.agentRows()) - 1
	m.agents.cursor = last - 1
	press("ctrl+d")
	assert.Equal(t, last, m.agents.cursor, "a page past the bottom lands on the last row")
}
