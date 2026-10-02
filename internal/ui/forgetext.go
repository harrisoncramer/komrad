package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func describeForgeLogin(login string) string {
	if login == "" {
		return "someone"
	}

	return "@" + login
}

// plainForgeText is text a stranger wrote with its escape sequences and control characters taken
// out, so it can never restyle the panel or reach the terminal.
func plainForgeText(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case r < ' ', r == 0x7f, r >= 0x80 && r <= 0x9f:
			return -1
		default:
			return r
		}
	}, ansi.Strip(text))
}
