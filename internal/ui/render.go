package ui

import (
	"fmt"
	"os"
	"strings"

	transcriptstore "weave/internal/store/transcript"

	"github.com/charmbracelet/glamour"
)

func render(markdown string, wrap int) string {
	options := []glamour.TermRendererOption{glamour.WithStyles(stageStyle)}
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

	return rendered
}

// markSpans writes the search's own markers around the stretches a hit matched on, before the
// markdown is rendered, since the rendering reflows the text and the marks have to travel with
// the words rather than with the lines they started on.
func markSpans(content string, spans []span) string {
	if len(spans) == 0 {
		return content
	}

	var b strings.Builder
	at := 0
	for _, s := range spans {
		if s.start < at || s.end > len(content) {
			continue
		}

		b.WriteString(content[at:s.start])
		b.WriteString(transcriptstore.SnippetOpen)
		b.WriteString(content[s.start:s.end])
		b.WriteString(transcriptstore.SnippetClose)
		at = s.end
	}
	b.WriteString(content[at:])

	return b.String()
}

// scrubMarks blanks the search's own markers wherever a transcript holds one itself, since a
// stray marker would pair with a real match's closing one, and a space rather than nothing
// keeps the byte offsets the search reports pointing where they did.
var scrubMarks = strings.NewReplacer(transcriptstore.SnippetOpen, " ", transcriptstore.SnippetClose, " ").Replace

// renderTranscript draws a transcript as the markdown it is written in, with the matched words
// styled, and reports the line the first of them ended up on.
func renderTranscript(conversation string, spans []span, width int) (string, int) {
	rendered := render(markSpans(conversation, spans), width)

	line := markedLine(rendered)
	if line == 0 && len(spans) > 0 {
		// The rendering drops some constructs whole, a link's destination and an HTML tag among
		// them, so a marker inside one goes with it and a guess says more about where the match
		// sat than the top of a transcript thousands of lines long.
		line = estimatedLine(rendered, spans[0].start, len(conversation))
	}

	return styleMarks(rendered), line
}

func markedLine(rendered string) int {
	for i, row := range strings.Split(rendered, "\n") {
		if strings.Contains(row, transcriptstore.SnippetOpen) {
			return i + 1
		}
	}

	return 0
}

// estimatedLine places an offset in the source at the same fraction of the rendered lines,
// which is as much as can be said about a match the rendering dropped.
func estimatedLine(rendered string, at, size int) int {
	if size <= 0 || at < 0 {
		return 0
	}

	rows := strings.Count(rendered, "\n") + 1

	return min(max(rows*at/size, 1), rows)
}

func writeStageFile(path, content string) bool {
	return os.WriteFile(path, []byte(content), 0o644) == nil
}

func pagerArgv(path string, line int) []string {
	argv := []string{"less", "-R", "-X", "-Ps"}
	if line > 1 {
		argv = append(argv, fmt.Sprintf("+%d", line))
	}

	return append(argv, path)
}
