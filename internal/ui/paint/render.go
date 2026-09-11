package paint

import (
	"strings"

	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
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

// Render draws markdown in the panel's own colours.
func Render(markdown string, wrap int) string {
	return renderWith(markdown, wrap, StageStyle)
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

// markSpans writes the search's own markers around the stretches a hit matched on, before the
// markdown is rendered and reflows the text under them.
func markSpans(content string, spans []Span) string {
	if len(spans) == 0 {
		return content
	}

	var b strings.Builder
	at := 0
	for _, s := range spans {
		if s.Start < at || s.End > len(content) {
			continue
		}

		b.WriteString(content[at:s.Start])
		b.WriteString(MarkOpen)
		b.WriteString(content[s.Start:s.End])
		b.WriteString(MarkClose)
		at = s.End
	}
	b.WriteString(content[at:])

	return b.String()
}

// ScrubMarks blanks the search's own markers wherever a transcript holds one itself, using a
// space to keep the byte offsets the search reports pointing where they did.
var ScrubMarks = strings.NewReplacer(MarkOpen, " ", MarkClose, " ").Replace

// RenderTranscript draws a transcript as the markdown it is written in, with the matched words
// styled, and reports the line the first of them ended up on.
func RenderTranscript(conversation string, spans []Span, width int) (string, int) {
	rendered := Render(markSpans(conversation, spans), width)

	line := markedLine(rendered)
	if line == 0 && len(spans) > 0 {
		// The rendering drops some constructs whole, a link's destination and an HTML tag among
		// them, so a marker inside one goes with it and a guess says more about where the match
		// sat than the top of a transcript thousands of lines long.
		line = estimatedLine(rendered, spans[0].Start, len(conversation))
	}

	return styleMarks(rendered, stageMatchStyle), line
}

// markedLine is the line the first marked stretch fell on, counting from one, or zero when none
// survived the rendering.
func markedLine(rendered string) int {
	for i, row := range strings.Split(rendered, "\n") {
		if strings.Contains(row, MarkOpen) {
			return i + 1
		}
	}

	return 0
}

// estimatedLine places an offset in the source at the same fraction of the rendered lines.
func estimatedLine(rendered string, at, size int) int {
	if size <= 0 || at < 0 {
		return 0
	}

	rows := strings.Count(rendered, "\n") + 1

	return min(max(rows*at/size, 1), rows)
}
