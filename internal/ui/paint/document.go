package paint

import (
	"strings"

	"github.com/charmbracelet/glamour/ansi"
)

// DocRows renders markdown into a column of its own, a row at a time.
func DocRows(markdown string, width int) []string {
	return DocStyledRows(markdown, width, StageStyle)
}

// DocStyledRows renders markdown a row at a time in the style it is handed.
func DocStyledRows(markdown string, width int, style ansi.StyleConfig) []string {
	return strings.Split(strings.TrimRight(renderWith(markdown, width, style), "\n"), "\n")
}
