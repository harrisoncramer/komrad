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

// TestValidateKeybindingsReadsTheGlobalKeys covers the keys routeGlobalKey answers before any
// screen's own table, where the shadowed action is the screen's rather than the global one.
func TestValidateKeybindingsReadsTheGlobalKeys(t *testing.T) {
	tests := []struct {
		name   string
		rebind func(*config.Keybindings)
		names  string
	}{
		{
			name:   "the help toggle taking the quit key, which leaves the screen with no quit",
			rebind: func(k *config.Keybindings) { k.Help.Toggle = []string{"ctrl+c"} },
			names:  "the help toggle",
		},
		{
			name:   "the log taking the rename key",
			rebind: func(k *config.Keybindings) { k.Logs.Open = []string{"r"} },
			names:  "the log",
		},
		{
			name:   "the terminal taking a movement key",
			rebind: func(k *config.Keybindings) { k.Terminal.Open = []string{"k"} },
			names:  "the terminal",
		},
		{
			name:   "the details focus taking the filter key",
			rebind: func(k *config.Keybindings) { k.Details.Focus = []string{filterOpenKey} },
			names:  "the details focus",
		},
		{
			name:   "the key that reads the messages, which is not configurable at all",
			rebind: func(k *config.Keybindings) { k.Details.Copy = []string{messagesFocusKey} },
			names:  "the key that reads the messages",
		},
		{
			name:   "the key that pushes a fixture into the messages",
			rebind: func(k *config.Keybindings) { k.Agents.Pause = []string{messagesMockKey} },
			names:  "the key that pushes a fixture into the messages",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := config.DefaultKeybindings()
			tt.rebind(&keys)

			err := ValidateKeybindings(keys)

			require.Error(t, err, "this key is answered before the screen's own table is reached")
			assert.Contains(t, err.Error(), tt.names)
		})
	}
}

// TestValidateKeybindingsAllowsAKeyOnlyATypingModeReads covers the keys whose only literal arms
// sit inside a mode that takes the whole keyboard, where no binding runs whatever key it is on.
func TestValidateKeybindingsAllowsAKeyOnlyATypingModeReads(t *testing.T) {
	tests := []struct {
		name   string
		rebind func(*config.Keybindings)
	}{
		{
			name:   "tab, which only the compose box reads",
			rebind: func(k *config.Keybindings) { k.Review.Resolve = []string{"tab"} },
		},
		{
			name:   "backspace, which only the filter and the search box read",
			rebind: func(k *config.Keybindings) { k.Agents.Hide = []string{"backspace"} },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := config.DefaultKeybindings()
			tt.rebind(&keys)

			assert.NoError(t, ValidateKeybindings(keys),
				"the arm that reads this key runs in a mode where no binding runs at all")
		})
	}
}

// TestGlobalArmsAnswerForEveryKeyTheRouterTakes presses every configured key against every screen
// and fails on one routeGlobalKey answers that globalArms does not name.
func TestGlobalArmsAnswerForEveryKeyTheRouterTakes(t *testing.T) {
	keys := config.DefaultKeybindings()
	pressable := configuredKeys(t, keys)

	for _, s := range []screen{screenAgents, screenSchedules, screenArchive, screenReview, screenWorktrees} {
		t.Run(screenNames[s], func(t *testing.T) {
			var modelled []string
			for _, arm := range globalArms(s, keys) {
				modelled = append(modelled, arm.keys...)
			}

			for _, pressed := range pressable {
				// A fresh model every press, since an arm that answered will have moved the panel on.
				_, _, taken := driftModel(t, s).routeGlobalKey(keyMsgOf(t, pressed))
				if !taken {
					continue
				}

				assert.Contains(t, modelled, pressed,
					"routeGlobalKey answers %q on %s ahead of the screen's own keys, and globalArms does not name it",
					pressed, screenNames[s])
			}
		})
	}
}

// TestValidateGitMenuKeysReadsTheKeysTheMenuIsDrawnOver covers an entry bound to a key the form
// around it needs, which the menu would answer first.
func TestValidateGitMenuKeysReadsTheKeysTheMenuIsDrawnOver(t *testing.T) {
	tests := []struct {
		name   string
		rebind func(*config.Keybindings)
		names  string
	}{
		{
			name:   "the key that takes the menu down",
			rebind: func(k *config.Keybindings) { k.Git.Rebase = []string{"esc"} },
			names:  "the key that takes a form down",
		},
		{
			name:   "the key that picks the entry under the cursor",
			rebind: func(k *config.Keybindings) { k.Git.Rebase = []string{"enter"} },
			names:  "the key that picks the entry under the cursor",
		},
		{
			name:   "the key that answers it",
			rebind: func(k *config.Keybindings) { k.Git.Rebase = []string{"ctrl+s"} },
			names:  "the key that answers a form",
		},
		{
			name:   "the key that walks the entries",
			rebind: func(k *config.Keybindings) { k.Git.Rebase = []string{"j"} },
			names:  "the key that moves down a form's list",
		},
		{
			name:   "the pull request entry on the key that takes the menu down",
			rebind: func(k *config.Keybindings) { k.Git.OpenPullRequest = []string{"esc"} },
			names:  "open_pull_request",
		},
		{
			name:   "two entries on one key, which the first answers every press of",
			rebind: func(k *config.Keybindings) { k.Git.OpenPullRequest = []string{"r"} },
			names:  "the menu's rebase entry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := config.DefaultKeybindings()
			tt.rebind(&keys)

			err := ValidateKeybindings(keys)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.names)
		})
	}
}
