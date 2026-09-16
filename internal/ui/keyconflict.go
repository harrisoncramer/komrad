package ui

import (
	"fmt"

	"weave/config"

	"github.com/charmbracelet/bubbles/key"
)

// Explainer: one key on two actions is how the shipped bindings put three actions on the archive
// key and how esc both clears a filter and steps back, and it works because the arm that reads
// the row or the filter comes first and the one that answers whatever is on screen comes last.
// The dispatch takes the first arm that answers, so an arm with no guard answers every press and
// nothing after it on that key can run. That is what this refuses, and it needs the tables rather
// than the file, which is why it is here rather than in the loader. The keys routeGlobalKey owns
// are read before any screen's table, so each screen is walked as those arms followed by its own.

// screenNames are the screens as an error names them.
var screenNames = map[screen]string{
	screenAgents:    "the agents list",
	screenSchedules: "the schedules",
	screenArchive:   "the archive",
	screenMail:      "the mailbox",
	screenReview:    "the review",
	screenSearch:    "the search",
}

// dispatchArm is one arm of the order a press is offered to, described for the error it would be
// named in and guarded where it answers only some of the presses that reach it.
type dispatchArm struct {
	describe string
	keys     []string
	guarded  bool
}

// screenActions is the table each screen dispatches through, which validateScreenKeys walks ahead
// of nothing and the panel walks behind routeGlobalKey.
func screenActions(s screen) (actionSpec, bool) {
	switch s {
	case screenAgents:
		return agentsActions(), true
	case screenSchedules:
		return schedulesActions(), true
	case screenArchive:
		return archiveActions(), true
	case screenMail:
		return mailActions(), true
	case screenReview:
		return reviewActions(), true
	case screenSearch:
		return actionSpec{}, false
	}

	return actionSpec{}, false
}

// ValidateKeybindings rejects the bindings a screen could not dispatch.
func ValidateKeybindings(keys config.Keybindings) error {
	for _, s := range []screen{screenAgents, screenSchedules, screenArchive, screenMail, screenReview} {
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

	for _, k := range keys.Rebase {
		if taken, ok := reserved[k]; ok {
			return fmt.Errorf("keybindings: the git menu's rebase is bound to %q, which is %s, and the menu answers it first",
				k, taken)
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

	spec, ok := screenActions(s)
	if !ok {
		return arms
	}

	for _, a := range spec.table {
		arms = append(arms, dispatchArm{
			describe: "an action of the screen's own",
			keys:     a.keys(keys),
			guarded:  a.when != nil,
		})
	}

	return arms
}

// globalArms are the keys routeGlobalKey answers on one screen, in the order it reads them, all of
// them ahead of that screen's own table.
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

	if s.spec().details != nil {
		arms = append(arms,
			// Neither of these is configurable, and both are read ahead of the screen's own keys.
			dispatchArm{describe: "the key that reads the messages", keys: []string{messagesFocusKey}},
			dispatchArm{describe: "the key that pushes a fixture into the messages", keys: []string{messagesMockKey}},
			dispatchArm{describe: "the details focus", keys: keys.Details.Focus},
			// The copy key answers only while the details hold the cursor, which is a mode the two
			// message keys above are read ahead of.
			dispatchArm{
				describe: "the details copy",
				keys:     keys.Details.Copy,
				guarded:  true,
			},
		)
	}

	return arms
}
