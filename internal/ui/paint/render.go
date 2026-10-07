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

type RenderTranscriptParams struct {
	Conversation string
	Spans        []Span
	Open         int
	Width        int
}

// RenderTranscript draws a transcript as the markdown it is written in, with the matched words
// styled, and reports the line the opened one of them ended up on.
func (p Palette) RenderTranscript(params RenderTranscriptParams) (string, int) {
	conversation, spans := params.Conversation, params.Spans
	rendered := p.Render(markSpans(conversation, spans), params.Width)

	open := min(max(params.Open, 0), max(len(spans)-1, 0))
	line := markedLine(rendered, open)
	if line == 0 && len(spans) > 0 {
		// The rendering drops some constructs whole, a link's destination and an HTML tag among
		// them, so a marker inside one goes with it and a guess says more about where the match
		// sat than the top of a transcript thousands of lines long.
		line = estimatedLine(rendered, spans[open].Start, len(conversation))
	}

	return styleMarks(rendered, p.stageMatch()), line
}

// markedLine is the line the nth marked stretch fell on, counting from one, or zero when it did
// not survive the rendering.
func markedLine(rendered string, nth int) int {
	for i, row := range strings.Split(rendered, "\n") {
		marks := strings.Count(row, MarkOpen)
		if nth < marks {
			return i + 1
		}
		nth -= marks
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
