package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestTheFocusKeyIsGivenBackOnTheWayOut covers the key the panel holds in the root table of a
// tmux server that outlives it, which every pane loses for as long as it stays bound.
func TestTheFocusKeyIsGivenBackOnTheWayOut(t *testing.T) {
	tmux := &bindingTmux{}
	popup := NewPopupWindow(tmux, "weave")

	popup.HoldFocusKey("%3", "%9")
	popup.ReleaseFocusKey()

	assert.Equal(t, []string{FocusKey}, tmux.unbound,
		"the panel is going, and tmux keeps a root binding until the server dies")
}

// TestReleasingAFocusKeyNobodyHeldSaysNothingToTmux covers the panel exiting from a screen that
// stages no agent, where there is no binding to give back.
func TestReleasingAFocusKeyNobodyHeldSaysNothingToTmux(t *testing.T) {
	tmux := &bindingTmux{}
	popup := NewPopupWindow(tmux, "weave")

	popup.ReleaseFocusKey()

	assert.Empty(t, tmux.unbound)
}
