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

// TestNoScreenBindsOneKeyTwiceUnderAUserConfig covers the invariant the default bindings are held
// to, which nothing holds a user's bindings to.
func TestNoScreenBindsOneKeyTwiceUnderAUserConfig(t *testing.T) {
	m := helpModel()
	m.screen = screenAgents
	m.keys = config.DefaultKeybindings()
	m.keys.Agents.New = []string{"j"}

	held := map[string]string{}

	for _, binding := range m.agentHelp().full {
		for _, k := range binding.Keys() {
			desc := binding.Help().Desc
			if was, taken := held[k]; taken {
				assert.Failf(t, "one key, two actions",
					"%q is bound to both %q and %q", k, was, desc)

				continue
			}

			held[k] = desc
		}
	}
}

// TestMovementUpSurvivesAKeyItSharesWithDown covers the actions that take two keys and decide
// which half ran by reading one of them, the other half being the fallthrough.
func TestMovementUpSurvivesAKeyItSharesWithDown(t *testing.T) {
	m := selectionTree()
	m.keys.Nav.Up = []string{"j"}
	m.keys.Nav.Down = []string{"j"}
	m.tree.cursor = 3

	after := press(t, m, keyMsg("j"))

	assert.Equal(t, 2, after.tree.cursor,
		"the key is bound to up as well, and up is the half that never runs")
}

// TestFormSubmitDoesNotEatATypedKey covers the form path, which reads the submit binding before
// the key reaches the field it was typed into.
func TestFormSubmitDoesNotEatATypedKey(t *testing.T) {
	m := driftModel(t, screenAgents)
	m.keys = config.DefaultKeybindings()
	m.keys.Form.Submit = []string{"s"}

	opened, _ := m.route(keyMsgOf(t, "c"))
	withForm, ok := opened.(model)
	require.True(t, ok)
	require.True(t, withForm.form.isActive(), "the new-agent form is up")

	filled, _ := withForm.route(keyMsgOf(t, "a"))
	started, ok := filled.(model)
	require.True(t, ok)

	_, cmd := started.route(keyMsgOf(t, "s"))

	assert.Nil(t, cmd,
		"an s typed into the prompt submitted the form, which for this form creates an agent")
}

// TestAChangedFocusKeyGivesTheOldOneBack covers the focus key on weave's own tmux server, which
// outlives the panel that bound it, so the key the config no longer names stays taken.
func TestAChangedFocusKeyGivesTheOldOneBack(t *testing.T) {
	tmux := &bindingTmux{}
	popup := NewPopupWindow(tmux, "weave")

	popup.HoldFocusKey("C-y", "%3", "%9")
	popup.HoldFocusKey("C-o", "%3", "%9")

	assert.Contains(t, tmux.unbound, "C-y",
		"the key the previous config named is still bound in the root table, and tmux keeps it until the server dies")

	_, stillBound := tmux.bound["root:C-y"]
	assert.False(t, stillBound)
}
