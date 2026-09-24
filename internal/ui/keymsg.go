package ui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
)

// keyMsg is the press bubbletea delivers for a key as a binding spells it.
func keyMsg(key string) tea.KeyPressMsg {
	var msg tea.KeyPressMsg

	rest := key
	for _, modifier := range []struct {
		prefix string
		mod    tea.KeyMod
	}{{"ctrl+", tea.ModCtrl}, {"alt+", tea.ModAlt}, {"shift+", tea.ModShift}} {
		if after, ok := strings.CutPrefix(rest, modifier.prefix); ok && after != "" {
			msg.Mod |= modifier.mod
			rest = after
		}
	}

	if code, ok := namedKeys[rest]; ok {
		msg.Code = code
		if code == tea.KeySpace && msg.Mod == 0 {
			msg.Text = " "
		}

		return msg
	}

	msg.Code, _ = utf8.DecodeRuneInString(rest)
	if utf8.RuneCountInString(rest) > 1 {
		msg.Code = tea.KeyExtended
	}

	if msg.Mod == 0 {
		msg.Text = rest
	}

	return msg
}

var namedKeys = map[string]rune{
	" ":         tea.KeySpace,
	"space":     tea.KeySpace,
	"enter":     tea.KeyEnter,
	"esc":       tea.KeyEscape,
	"tab":       tea.KeyTab,
	"backspace": tea.KeyBackspace,
	"delete":    tea.KeyDelete,
	"up":        tea.KeyUp,
	"down":      tea.KeyDown,
	"left":      tea.KeyLeft,
	"right":     tea.KeyRight,
	"pgup":      tea.KeyPgUp,
	"pgdown":    tea.KeyPgDown,
	"home":      tea.KeyHome,
	"end":       tea.KeyEnd,
}
