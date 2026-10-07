package paint

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// StripControls removes the escape sequences and control characters other than tabs from text the
// panel renders.
func StripControls(text string) string {
	return strings.Map(func(r rune) rune {
		if r != '\t' && unicode.IsControl(r) {
			return -1
		}

		return r
	}, ansi.Strip(text))
}
