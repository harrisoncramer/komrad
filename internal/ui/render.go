package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
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

var (
	highlightStyle = ansi.NewStyle().Reverse(true)
	matchTermStyle = ansi.NewStyle().Reverse(true).Bold().Underline(true)
)

// highlighted marks every line a search hit matched on, with the matched words themselves
// carried further out of the line, since a whole line under the cursor says the turn matched
// without saying which word did.
func highlighted(content string, spans []matchSpan) string {
	if len(spans) == 0 {
		return content
	}

	lines := strings.Split(content, "\n")

	at := 0
	for i, line := range lines {
		start, end := at, at+len(line)
		at = end + 1

		within := spansIn(spans, start, end)
		if len(within) == 0 {
			continue
		}

		lines[i] = highlightLine(line, start, within)
	}

	return strings.Join(lines, "\n")
}

func spansIn(spans []matchSpan, start, end int) []matchSpan {
	var within []matchSpan
	for _, span := range spans {
		if span.end > start && span.start < end {
			within = append(within, span)
		}
	}

	return within
}

func highlightLine(line string, base int, spans []matchSpan) string {
	var b strings.Builder

	at := 0
	for _, span := range spans {
		from, to := max(span.start-base, 0), min(span.end-base, len(line))
		if from >= to {
			continue
		}
		if from > at {
			b.WriteString(highlightStyle.Styled(line[at:from]))
		}
		b.WriteString(matchTermStyle.Styled(line[from:to]))
		at = to
	}

	if at < len(line) {
		b.WriteString(highlightStyle.Styled(line[at:]))
	}

	return b.String()
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
