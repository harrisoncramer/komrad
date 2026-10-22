package paint

import (
	"fmt"
	"strings"

	"charm.land/glamour/v2/ansi"
)

// DocRows renders markdown into a column of its own, a row at a time.
func (p Palette) DocRows(markdown string, width int) []string {
	return DocStyledRows(markdown, width, p.Stage())
}

// DocStyledRows renders markdown a row at a time in the style it is handed.
func DocStyledRows(markdown string, width int, style ansi.StyleConfig) []string {
	return strings.Split(strings.TrimRight(renderWith(markdown, width, style), "\n"), "\n")
}

// RenderDocument writes the markdown of a titled page with an optional notice, prose and footer.
func RenderDocument(title, notice, prose, footer string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", title)
	if notice != "" {
		b.WriteString(notice + "\n\n")
	}
	if prose != "" {
		b.WriteString(prose + "\n")
	}
	if footer != "" {
		b.WriteString("\n" + footer + "\n")
	}

	return b.String()
}
