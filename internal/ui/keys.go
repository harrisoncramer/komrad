package ui

import (
	"slices"

	"komrad/config"
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

// isTopHeld reports whether the last press opened the doubled top key and is waiting on its pair.
func (m *model) isTopHeld() bool {
	return m.topHeld
}

func (m *model) takeTopPress(pressed string) bool {
	held := m.topHeld
	m.topHeld = !held && isBound(pressed, m.keys.Nav.Top)

	return held && isBound(pressed, m.keys.Nav.Top)
}

func (m *model) dropTopPress() {
	m.topHeld = false
}

// fixedKeys is what every screen, menu and form answers.
func fixedKeys() config.FixedKeys {
	return config.Fixed()
}

func fixedBack(config.Keybindings) []string {
	return fixedKeys().Back
}

func fixedQuit(config.Keybindings) []string {
	return fixedKeys().Quit
}

func fixedOpen(config.Keybindings) []string {
	return fixedKeys().Open
}
