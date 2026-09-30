package ui

import (
	"strings"

	"komrad/internal/panes"
)

// pickFont is the picker's letters drawn five rows tall, each cell doubled across so a letter
// reads square.
var pickFont = map[string][]string{
	"d": {"██████ ", "██   ██", "██   ██", "██   ██", "██████ "},
	"f": {"███████", "██     ", "█████  ", "██     ", "██     "},
	"j": {"     ██", "     ██", "     ██", "██   ██", " █████ "},
	"k": {"██   ██", "██  ██ ", "█████  ", "██  ██ ", "██   ██"},
	"l": {"██     ", "██     ", "██     ", "██     ", "███████"},
	"s": {" ██████", "██     ", " █████ ", "     ██", "██████ "},
	"a": {" █████ ", "██   ██", "███████", "██   ██", "██   ██"},
}

// drawLetter is a target's letter as large as the target holds, falling back to the letter alone.
func drawLetter(letter string, rect panes.Rect) string {
	rows, ok := pickFont[letter]
	if !ok {
		return letter
	}

	wide := make([]string, len(rows))
	for i, row := range rows {
		var b strings.Builder
		for _, r := range row {
			b.WriteRune(r)
			b.WriteRune(r)
		}
		wide[i] = b.String()
	}

	if len([]rune(wide[0])) > rect.Width || len(wide) > rect.Height {
		return letter
	}

	return strings.Join(wide, "\n")
}
