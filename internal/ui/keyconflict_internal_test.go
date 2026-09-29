package ui

import (
	"testing"

	"komrad/config"

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
// the agents list.
func TestValidateKeybindingsReadsEveryScreen(t *testing.T) {
	tests := []struct {
		name    string
		rebind  func(*config.Keybindings)
		screens []screen
	}{
		{
			name:    "a review key taking a movement key",
			rebind:  func(k *config.Keybindings) { k.Review.Reload = k.Nav.Down },
			screens: []screen{screenReview},
		},
		{
			name:    "a diff key taking a movement key, on both screens that draw a diff",
			rebind:  func(k *config.Keybindings) { k.Diff.PanLeft = k.Nav.Down },
			screens: []screen{screenReview, screenDocuments},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := config.DefaultKeybindings()
			tt.rebind(&keys)

			require.Error(t, ValidateKeybindings(keys))

			for _, target := range tt.screens {
				err := validateScreenKeys(target, keys)

				require.Error(t, err)
				assert.Contains(t, err.Error(), screenNames[target])
			}
		})
	}
}

// TestValidateKeybindingsAllowsAGuardedArmInFront covers one key layered over two actions the way
// esc clears a standing filter and otherwise steps back.
func TestValidateKeybindingsAllowsAGuardedArmInFront(t *testing.T) {
	keys := config.DefaultKeybindings()
	keys.Agents.Hide = keys.Agents.Archive

	assert.NoError(t, ValidateKeybindings(keys),
		"both of these read the row under the cursor before they answer, so only one is ever live")
}

// TestValidateKeybindingsAllowsTheKindsKeyWhereNoRowOfKindsIsDrawn covers a screen that lists one
// kind, where the kinds key is free for its own table.
func TestValidateKeybindingsAllowsTheKindsKeyWhereNoRowOfKindsIsDrawn(t *testing.T) {
	keys := config.DefaultKeybindings()
	keys.Agents.Pause = keys.Kinds.Focus

	assert.NoError(t, ValidateKeybindings(keys), "only the archive answers the kinds key ahead of its own table")
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
			name:   "the backward view cycle",
			rebind: func(k *config.Keybindings) { k.Agents.Pause = []string{k.Cycle.Views[1]} },
			names:  "the view cycle",
		},
		{
			name:   "the key that reads the messages",
			rebind: func(k *config.Keybindings) { k.Agents.Pause = k.Messages.Open },
			names:  "the key that reads the messages",
		},
		{
			name:   "the key that pushes a fixture into the messages",
			rebind: func(k *config.Keybindings) { k.Agents.Pause = []string{messagesMockKey} },
			names:  "the key that pushes a fixture into the messages",
		},
		{
			name:   "an archive key on the row of kinds",
			rebind: func(k *config.Keybindings) { k.Archive.Restore = k.Kinds.Focus },
			names:  "the kinds focus",
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

// TestValidateSubmenuKeysReadsTheKeysTheMenuIsDrawnOver covers an entry bound to a key the form
// around it needs, which the menu would answer first.
func TestValidateSubmenuKeysReadsTheKeysTheMenuIsDrawnOver(t *testing.T) {
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
		{
			name:   "a subpage on the key that takes the menu down",
			rebind: func(k *config.Keybindings) { k.Subpages.Logs = []string{"esc"} },
			names:  "logs",
		},
		{
			name:   "two subpages on one key",
			rebind: func(k *config.Keybindings) { k.Subpages.Documents = []string{"r"} },
			names:  "the menu's review entry",
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
