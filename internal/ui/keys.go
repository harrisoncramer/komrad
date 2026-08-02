package ui

import (
	"slices"
)

// isBound reports whether the pressed key is one of the keys configured for a binding.
func isBound(pressed string, keys []string) bool {
	return slices.Contains(keys, pressed)
}
