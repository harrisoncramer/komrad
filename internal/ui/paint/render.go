package paint

import (
	"strings"

	"charm.land/glamour/v2"
	glamouransi "charm.land/glamour/v2/ansi"
	"github.com/charmbracelet/x/ansi"
)

const tabWidth = 4

// ScrubMarks blanks the search's own markers wherever a transcript holds one itself, using a
// space to keep the byte offsets the search reports pointing where they did.
var ScrubMarks = strings.NewReplacer(MarkOpen, " ", MarkClose, " ").Replace

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

// Render renders markdown in the colours of the region it is rendered in.
func (p Palette) Render(markdown string, wrap int) string {
	return renderWith(markdown, wrap, p.Stage())
}

// renderWith renders markdown in the style it is handed, hard-wrapped to the column so nothing a
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
	// than the column, and every caller renders the column itself rather than handing it to a pager
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

	var marked strings.Builder
	at := 0
	for _, span := range spans {
		if span.Start < at || span.End > len(content) {
			continue
		}

		marked.WriteString(content[at:span.Start])
		marked.WriteString(MarkOpen)
		marked.WriteString(content[span.Start:span.End])
		marked.WriteString(MarkClose)
		at = span.End
	}
	marked.WriteString(content[at:])

	return marked.String()
}

// RenderTranscriptParams is a transcript to render, the stretches of it to mark, and the column to
// render it in.
type RenderTranscriptParams struct {
	Conversation string
	Spans        []Span
	Width        int
}

// RenderTranscript renders a transcript as the markdown it is written in, with the matched words
// styled, and reports the line the first of them ended up on.
func (p Palette) RenderTranscript(params RenderTranscriptParams) (string, int) {
	rendered := p.Render(markSpans(params.Conversation, params.Spans), params.Width)

	line := markedLine(rendered)
	if line == 0 && len(params.Spans) > 0 {
		// The rendering drops some constructs whole, a link's destination and an HTML tag among
		// them, so a marker inside one goes with it and a guess says more about where the match
		// sat than the top of a transcript thousands of lines long.
		line = estimatedLine(rendered, params.Spans[0].Start, len(params.Conversation))
	}

	return styleMarks(rendered, p.stageMatch()), line
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
