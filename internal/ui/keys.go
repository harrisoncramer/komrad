package ui

import (
	"slices"
)

func isBound(pressed string, keys []string) bool {
	return slices.Contains(keys, pressed)
}

func (m *model) takeTopPress(pressed string) bool {
	held := m.topHeld
	m.topHeld = !held && isBound(pressed, m.keys.Nav.Top)

	return held && isBound(pressed, m.keys.Nav.Top)
}
