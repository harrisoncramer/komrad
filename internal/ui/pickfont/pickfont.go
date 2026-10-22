// Package pickfont is the pane picker's letters drawn large.
package pickfont

import (
	"maps"
	"slices"
	"strings"

	"komrad/internal/panes"
)

// font is the letters rendered five rows tall, each cell doubled across so a letter reads square.
var font = map[string][]string{
	"d": {"██████ ", "██   ██", "██   ██", "██   ██", "██████ "},
	"f": {"███████", "██     ", "█████  ", "██     ", "██     "},
	"j": {"     ██", "     ██", "     ██", "██   ██", " █████ "},
	"k": {"██   ██", "██  ██ ", "█████  ", "██  ██ ", "██   ██"},
	"l": {"██     ", "██     ", "██     ", "██     ", "███████"},
	"s": {" ██████", "██     ", " █████ ", "     ██", "██████ "},
	"a": {" █████ ", "██   ██", "███████", "██   ██", "██   ██"},
}

// Letters is every letter the font draws.
func Letters() []string {
	return slices.Sorted(maps.Keys(font))
}

// Render is a letter as large as a rect holds, falling back to the letter alone.
func Render(letter string, rect panes.Rect) string {
	rows, ok := font[letter]
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
