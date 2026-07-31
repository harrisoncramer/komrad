package ui

import (
	"slices"
)

// bound reports whether the pressed key is one of the keys configured for a binding.
func bound(pressed string, keys []string) bool {
	return slices.Contains(keys, pressed)
}
