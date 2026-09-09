package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	transcriptstore "weave/internal/store/transcript"

	"github.com/charmbracelet/glamour"
	glamouransi "github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/x/ansi"
)

const tabWidth = 4

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

func render(markdown string, wrap int) string {
	return renderWith(markdown, wrap, stageStyle)
}

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

// scrubMarks blanks the search's own markers wherever a transcript holds one itself, using a
// space to keep the byte offsets the search reports pointing where they did.
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

	return styleMarks(rendered, stageMatchStyle), line
}

func markedLine(rendered string) int {
	for i, row := range strings.Split(rendered, "\n") {
		if strings.Contains(row, transcriptstore.SnippetOpen) {
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

// stageFilePath names a screenful for the panel that wrote it, in the one temporary directory
// every weave on the machine shares.
func stageFilePath(name string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("weave-%s-%d.ansi", name, os.Getpid()))
}

// writeStageFile renames the frame into place, so a drawer reading while the panel writes gets
// the frame before or the frame after rather than half of each.
func writeStageFile(path, content string) bool {
	temp := path + ".tmp"
	if os.WriteFile(temp, []byte(content), 0o644) != nil {
		return false
	}

	return os.Rename(temp, path) == nil
}

func pagerArgv(path string, line int) []string {
	argv := []string{"less", "-R", "-X", "-Ps"}
	if line > 1 {
		argv = append(argv, fmt.Sprintf("+%d", line))
	}

	return append(argv, path)
}
