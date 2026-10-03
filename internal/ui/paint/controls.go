package paint

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// StripControls removes the escape sequences and control characters other than tabs from text the
// panel draws.
func StripControls(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\t':
			return r
		case r < ' ' || r == 0x7f:
			return -1
		default:
			return r
		}
	}, ansi.Strip(text))
}
