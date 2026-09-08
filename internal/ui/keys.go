package ui

import (
	"slices"
)

// keyProbe is the drift test's seam for asking which bindings a press reached, since a key can be
// handled without altering anything drawn, and is nil everywhere but that test.
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
