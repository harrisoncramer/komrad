package review

import (
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
)

// withSection is a popup body with a section under it, such as the menu or the editor, or the section alone under no body.
func withSection(body, section string, inner int) string {
	if body == "" {
		return section
	}

	return strings.TrimRight(body, "\n") + "\n" + paint.Panel.Style(paint.ToneMuted).Render(strings.Repeat(glyph.Rule, inner)) + "\n" + section
}
