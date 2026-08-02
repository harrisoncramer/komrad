package ui

import (
	"slices"
)

// isBound reports whether the pressed key is one of the keys configured for a binding.
func isBound(pressed string, keys []string) bool {
	return slices.Contains(keys, pressed)
}

// takeTopPress answers a press of the jump-to-top key, which acts on the second press the way
// vim's gg does. It reports whether this press completed the pair, and otherwise records a
// first press for the next key to complete or drop.
func (m *model) takeTopPress(pressed string) bool {
	held := m.topHeld
	m.topHeld = !held && isBound(pressed, m.keys.Nav.Top)

	return held && isBound(pressed, m.keys.Nav.Top)
}
