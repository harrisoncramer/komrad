package ui

import (
	"slices"
	"strings"

	"komrade/config"

	tea "charm.land/bubbletea/v2"
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

// typedText is what a press types into a line, empty for a press that types nothing.
func typedText(key tea.KeyPressMsg) string {
	if key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModAlt) {
		return ""
	}

	return key.Text
}

// pastedLine is pasted text as it goes into a line of one row.
func pastedLine(paste tea.PasteMsg) string {
	return strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(paste.Content)
}

// isTopHeld reports whether the last press opened the doubled top key and is waiting on its pair.
func (m model) isTopHeld() bool {
	return m.topHeld
}

func (m *model) takeTopPress(pressed string) bool {
	held := m.topHeld
	m.topHeld = !held && isBound(pressed, m.keys.Nav.Top)

	return held && isBound(pressed, m.keys.Nav.Top)
}

// formKeys is what a form answers, fixed rather than configured.
func formKeys() config.FormKeys {
	return config.FixedFormKeys()
}
