package ui

import (
	"fmt"

	"weave/config"

	"github.com/charmbracelet/bubbles/key"
)

// screenNames are the screens as an error names them.
var screenNames = map[screen]string{
	screenAgents:    "the agents list",
	screenSchedules: "the schedules",
	screenArchive:   "the archive",
	screenReview:    "the review",
	screenSearch:    "the search",
	screenWorktrees: "the worktrees",
}

// dispatchArm is one arm of the order a press is offered to, described for the error it would be
// named in and guarded where it answers only some of the presses that reach it.
type dispatchArm struct {
	describe string
	keys     []string
	guarded  bool
}

// dispatching is a screen's table as the keybinding check walks it.
type dispatching interface {
	dispatchArms(keys config.Keybindings) []dispatchArm
}

// actionsOf is the table each screen dispatches through, walked behind the global keys.
func actionsOf(s screen) (dispatching, bool) {
	switch s {
	case screenAgents:
		return (&agentsScreen{}).actions(), true
	case screenSchedules:
		return (&schedulesScreen{}).actions(), true
	case screenArchive:
		return (&archiveScreen{}).actions(), true
	case screenReview:
		return (&reviewScreen{}).actions(), true
	case screenWorktrees:
		return (&worktreesScreen{}).actions(), true
	case screenSearch:
		return (&searchScreen{}).actions(), true
	}

	return nil, false
}

// ValidateKeybindings rejects the bindings a screen could not dispatch.
func ValidateKeybindings(keys config.Keybindings) error {
	for _, s := range []screen{screenAgents, screenSchedules, screenArchive, screenReview, screenWorktrees, screenSearch} {
		if err := validateScreenKeys(s, keys); err != nil {
			return err
		}
	}

	return validateGitMenuKeys(keys.Git)
}

// gitMenuReserved is every key the menu is drawn over, named as the error would name it.
func gitMenuReserved() map[string]string {
	km := gitMenuKeyMap(formKeys())

	arms := []struct {
		describe string
		binding  key.Binding
	}{
		{describe: "the key that takes a form down", binding: km.Quit},
		{describe: "the key that picks the entry under the cursor", binding: km.Select.Next},
		{describe: "the key that answers a form", binding: km.Select.Submit},
		{describe: "the key that moves up a form's list", binding: km.Select.Up},
		{describe: "the key that moves down a form's list", binding: km.Select.Down},
	}

	reserved := map[string]string{}
	for _, arm := range arms {
		for _, k := range arm.binding.Keys() {
			reserved[k] = arm.describe
		}
	}

	return reserved
}

// validateGitMenuKeys refuses an entry's key that the menu around it needs.
func validateGitMenuKeys(keys config.GitKeys) error {
	reserved := gitMenuReserved()

	entries := []struct {
		name string
		keys []string
	}{
		{name: "rebase", keys: keys.Rebase},
		{name: "open_pull_request", keys: keys.OpenPullRequest},
	}
	for _, entry := range entries {
		for _, k := range entry.keys {
			if taken, ok := reserved[k]; ok {
				return fmt.Errorf("keybindings: the git menu's %s is bound to %q, which is %s, and the menu answers it first",
					entry.name, k, taken)
			}

			reserved[k] = "the menu's " + entry.name + " entry"
		}
	}

	return nil
}

// validateScreenKeys walks one screen in dispatch order and finds a key an earlier unguarded arm
// answers ahead of a later one.
func validateScreenKeys(s screen, keys config.Keybindings) error {
	// answered holds, per key, the arm that takes every press of it and the arm's own place in the
	// order, so an arm is not read as shadowing itself.
	type shadow struct {
		describe string
		at       int
	}

	answered := map[string]shadow{}

	for at, arm := range dispatchOrder(s, keys) {
		for _, key := range arm.keys {
			if taken, ok := answered[key]; ok && taken.at != at {
				if taken.describe == arm.describe {
					return fmt.Errorf("keybindings: on %s, %q is bound to two of the screen's own actions, and the first answers every press of it, so the second never runs",
						screenNames[s], key)
				}

				return fmt.Errorf("keybindings: on %s, %q is bound to both %s and %s, and %s answers every press of it, so the other never runs",
					screenNames[s], key, taken.describe, arm.describe, taken.describe)
			}

			if !arm.guarded {
				answered[key] = shadow{describe: arm.describe, at: at}
			}
		}
	}

	return nil
}

// dispatchOrder is every arm one screen offers a press to, in the order it offers them.
func dispatchOrder(s screen, keys config.Keybindings) []dispatchArm {
	arms := globalArms(s, keys)

	spec, ok := actionsOf(s)
	if !ok {
		return arms
	}

	return append(arms, spec.dispatchArms(keys)...)
}

// globalArms are the keys the panel answers on one screen ahead of that screen's own table, in
// the order it reads them.
func globalArms(s screen, keys config.Keybindings) []dispatchArm {
	arms := []dispatchArm{{describe: "the help toggle", keys: keys.Help.Toggle}}

	if s.opensInfoPane() {
		arms = append(arms,
			dispatchArm{describe: "the log", keys: keys.Logs.Open},
			dispatchArm{describe: "the earlier log", keys: keys.Logs.Older},
			dispatchArm{describe: "the terminal", keys: keys.Terminal.Open},
		)
	}

	// The column answers only while the block beside the list is holding one.
	arms = append(arms,
		dispatchArm{
			describe: "the column scroll",
			keys:     keys.Column.ScrollDown,
			guarded:  true,
		},
		dispatchArm{
			describe: "the column scroll",
			keys:     keys.Column.ScrollUp,
			guarded:  true,
		},
	)

	if _, ok := s.adapter().(tabbed); ok {
		arms = append(arms, dispatchArm{describe: "the tabs focus", keys: keys.Tabs.Focus})
	}

	if _, ok := s.adapter().(detailed); ok {
		arms = append(arms,
			// Neither of these is configurable, and both are read ahead of the screen's own keys.
			dispatchArm{describe: "the key that reads the messages", keys: []string{messagesFocusKey}},
			dispatchArm{describe: "the key that pushes a fixture into the messages", keys: []string{messagesMockKey}},
			dispatchArm{describe: "the details focus", keys: keys.Details.Focus},
			// The copy key answers only while the details hold the cursor.
			dispatchArm{
				describe: "the details copy",
				keys:     keys.Details.Copy,
				guarded:  true,
			},
		)
	}

	return arms
}
