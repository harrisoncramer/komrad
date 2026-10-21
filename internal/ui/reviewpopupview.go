package ui

import (
	"strings"

	"komrad/internal/glyph"
)

// withSection is a popup body with a section under it, such as the menu or the editor, or the section alone under no body.
func withSection(body, section string, inner int) string {
	if body == "" {
		return section
	}

	return strings.TrimRight(body, "\n") + "\n" + dimStyle().Render(strings.Repeat(glyph.Rule, inner)) + "\n" + section
}
