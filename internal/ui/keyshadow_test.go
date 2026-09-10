package ui

import (
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// reachedBinding presses one key and reports the binding list the dispatch matched it against.
func reachedBinding(t *testing.T, m model, pressed string) []string {
	t.Helper()

	var reached []string

	keyProbe = func(keys []string) {
		if reached == nil {
			reached = keys
		}
	}
	defer func() { keyProbe = nil }()

	m.route(keyMsgOf(t, pressed))

	return reached
}

// TestARebindIsNotShadowedByMovement covers a screen action rebound onto a movement key, which
// the agents dispatch reads after movement and so never runs.
func TestARebindIsNotShadowedByMovement(t *testing.T) {
	m := driftModel(t, screenAgents)
	m.keys = config.DefaultKeybindings()
	m.keys.Agents.New = []string{"j"}

	reached := reachedBinding(t, m, "j")

	require.NotNil(t, reached, "the press reached no binding at all")
	assert.Equal(t, []string{"j"}, reached,
		"the key the file gives to new was answered by movement instead, and nothing says so")
}

// TestQuitSurvivesAHelpRebind covers the help toggle taking the quit key, the global arm reading
// it before the screen's own table.
func TestQuitSurvivesAHelpRebind(t *testing.T) {
	m := driftModel(t, screenAgents)
	m.keys = config.DefaultKeybindings()
	m.keys.Help.Toggle = []string{"ctrl+c"}

	next, cmd := m.route(keyMsgOf(t, "ctrl+c"))

	assert.NotNil(t, cmd, "quit is still bound to this key in the file, so pressing it quits")

	after, ok := next.(model)
	require.True(t, ok)
	assert.False(t, after.helpExpanded, "the legend was toggled instead of the panel quitting")
}
