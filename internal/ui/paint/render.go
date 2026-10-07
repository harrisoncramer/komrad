package paint

import (
	"strings"

	"charm.land/glamour/v2"
	glamouransi "charm.land/glamour/v2/ansi"
	"github.com/charmbracelet/x/ansi"
)

const tabWidth = 4

// expandTabs replaces tabs with the spaces that reach the next stop.
func expandTabs(text string) string {
	if !strings.Contains(text, "\t") {
		return text
	}

	var b strings.Builder
	column := 0
	for _, r := range text {
		switch r {
		case '\t':
			pad := tabWidth - column%tabWidth
			b.WriteString(strings.Repeat(" ", pad))
			column += pad
		case '\n':
			b.WriteRune(r)
			column = 0
		default:
			b.WriteRune(r)
			column++
		}
	}

	return b.String()
}

// Render draws markdown in the colours of the region it is drawn in.
func (p Palette) Render(markdown string, wrap int) string {
	return renderWith(markdown, wrap, p.Stage())
}

// renderWith draws markdown in the style it is handed, hard-wrapped to the column so nothing a
// word wrap will not break runs past it.
func renderWith(markdown string, wrap int, style glamouransi.StyleConfig) string {
	markdown = expandTabs(markdown)

	options := []glamour.TermRendererOption{glamour.WithStyles(style)}
	if wrap > 0 {
		options = append(options, glamour.WithWordWrap(wrap))
	}

	renderer, err := glamour.NewTermRenderer(options...)
	if err != nil {
		return markdown
	}

	rendered, err := renderer.Render(markdown)
	if err != nil {
		return markdown
	}

	// The markdown wraps on words, which leaves a code block or a link that will not break wider
	// than the column, and every caller draws the column itself rather than handing it to a pager
	// that would fold the overflow.
	if wrap > 0 {
		rendered = ansi.Hardwrap(rendered, wrap, true)
	}

	return rendered
}
