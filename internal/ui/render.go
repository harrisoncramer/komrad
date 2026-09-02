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

// renderTranscript draws a transcript as the markdown it is written in, with the matched words
// styled, and reports the line the first of them ended up on.
func renderTranscript(conversation string, spans []span, width int) (string, int) {
	rendered := render(markSpans(conversation, spans), width)

	line := 0
	for i, row := range strings.Split(rendered, "\n") {
		if strings.Contains(row, transcriptstore.SnippetOpen) {
			line = i + 1

			break
		}
	}

	return styleMarks(rendered), line
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
