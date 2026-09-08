package ui

import (
	"slices"
)

// keyProbe is the seam for asking which bindings a press reached, and is nil outside the drift
// test.
var keyProbe func(keys []string)

func isBound(pressed string, keys []string) bool {
	if !slices.Contains(keys, pressed) {
		return false
	}

	if keyProbe != nil {
		keyProbe(keys)
	}

	return true
}

func (m *model) takeTopPress(pressed string) bool {
	held := m.topHeld
	m.topHeld = !held && isBound(pressed, m.keys.Nav.Top)

	return held && isBound(pressed, m.keys.Nav.Top)
}
