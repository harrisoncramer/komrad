package ui

import (
	"fmt"
	"maps"
	"slices"

	"weave/config"
)

// Explainer: one key on two actions is how the shipped bindings put three actions on the archive
// key and how esc both clears a filter and steps back, and it works because the arm that reads
// the row or the filter comes first and the one that answers whatever is on screen comes last.
// The dispatch takes the first arm that answers, so an arm with no guard answers every press and
// nothing after it on that key can run. That is what this refuses, and it needs the tables rather
// than the file, which is why it is here rather than in the loader.

// ValidateKeybindings rejects the bindings a screen could not dispatch, which is a key an
// unguarded action answers ahead of another action bound to it.
func ValidateKeybindings(keys config.Keybindings) error {
	screens := map[string]actionSpec{
		"the agents list": agentsActions(),
		"the schedules":   schedulesActions(),
		"the archive":     archiveActions(),
		"the mailbox":     mailActions(),
		"the review":      reviewActions(),
	}

	for _, name := range slices.Sorted(maps.Keys(screens)) {
		if err := validateScreenKeys(name, screens[name], keys); err != nil {
			return err
		}
	}

	return nil
}

// validateScreenKeys finds a key an earlier unguarded arm of one screen's table answers ahead of
// a later arm bound to the same key.
func validateScreenKeys(screen string, spec actionSpec, keys config.Keybindings) error {
	answered := map[string]actionID{}

	for _, a := range spec.table {
		for _, key := range a.keys(keys) {
			if taken, ok := answered[key]; ok && taken != a.id {
				return fmt.Errorf("keybindings: %q is bound to two actions on %s and the first of them answers every press, so the second never runs",
					key, screen)
			}

			if a.when == nil {
				answered[key] = a.id
			}
		}
	}

	return nil
}
