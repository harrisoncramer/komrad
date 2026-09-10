package ui

import (
	"testing"

	"weave/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheShippedBindingsPassTheirOwnCheck covers the layering the defaults rely on, where the arm
// that reads the row or the filter comes first and the one that answers anything comes last.
func TestTheShippedBindingsPassTheirOwnCheck(t *testing.T) {
	require.NoError(t, ValidateKeybindings(config.DefaultKeybindings()))
}

// TestValidateKeybindingsFindsAShadowedAction covers a screen action rebound onto a key an
// earlier unguarded arm already answers, which the dispatch never reaches.
func TestValidateKeybindingsFindsAShadowedAction(t *testing.T) {
	keys := config.DefaultKeybindings()
	keys.Agents.New = []string{"j"}

	err := ValidateKeybindings(keys)

	require.Error(t, err, "movement answers every press, so the new-agent form could not be opened")
	assert.Contains(t, err.Error(), "the agents list")
	assert.Contains(t, err.Error(), `"j"`)
}

// TestValidateKeybindingsReadsEveryScreen covers a rebind that only collides somewhere other than
// the agents list, the review being the screen with the most keys on it.
func TestValidateKeybindingsReadsEveryScreen(t *testing.T) {
	keys := config.DefaultKeybindings()
	keys.Review.Reload = keys.Nav.Down

	err := ValidateKeybindings(keys)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "the review")
}

// TestValidateKeybindingsAllowsAGuardedArmInFront covers one key layered over two actions the way
// esc clears a standing filter and otherwise steps back.
func TestValidateKeybindingsAllowsAGuardedArmInFront(t *testing.T) {
	keys := config.DefaultKeybindings()
	keys.Agents.Hide = keys.Agents.Archive

	assert.NoError(t, ValidateKeybindings(keys),
		"both of these read the row under the cursor before they answer, so only one is ever live")
}
