package ui

import (
	"slices"

	"weave/config"
)

// keyProbe is the seam for asking which bindings a press reached, and is nil unless a test sets
// it.
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

// formKeys is what a form answers, fixed rather than configured because they are read before the
// key reaches the box being typed into.
func formKeys() config.FormKeys {
	return config.FixedFormKeys()
}
