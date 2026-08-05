package ui

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
)

// Terminal style detection would compete with the panel event loop for input.
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

	return dimNumbers(rendered)
}

// numberStyle is how a record field's leading "N)" is drawn once glamour has laid the
// line out. glamour's own Enumeration style never reaches this text: an ordered list
// item renders its marker through the surrounding block's style rather than its own,
// so recolouring Enumeration in stageStyle has no visible effect. Dimming it here
// after the fact is what actually sets the marker apart from the value beside it,
// which a digit key copies and so is left in the plain foreground.
var numberStyle = ansi.NewStyle().Faint()

// numberPrefix matches a record field's rendered "N) " marker, glamour's own left
// margin ahead of it included, so dimNumbers can dim just the marker and leave that
// margin and everything after it alone.
var numberPrefix = regexp.MustCompile(`^(\s*)(\d+\)) `)

// dimNumbers dims every record field's leading "N)" in already-rendered text. It is a
// no-op on a document with no such line, which is every document that is not a record.
func dimNumbers(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = numberPrefix.ReplaceAllStringFunc(line, func(match string) string {
			groups := numberPrefix.FindStringSubmatch(match)

			return groups[1] + numberStyle.Styled(groups[2]) + " "
		})
	}

	return strings.Join(lines, "\n")
}

// ANSI styling avoids terminal profile detection on the panel's input stream.
var highlightStyle = ansi.NewStyle().Reverse(true)

func highlighted(content string, line int) string {
	lines := strings.Split(content, "\n")
	if line < 1 || line > len(lines) {
		return content
	}

	lines[line-1] = highlightStyle.Styled(lines[line-1])

	return strings.Join(lines, "\n")
}

func writeStageFile(path, content string) bool {
	return os.WriteFile(path, []byte(content), 0o644) == nil
}

// The pager must remain running or tmux will close its pane.
func pagerArgv(path string, line int) []string {
	argv := []string{"less", "-R", "-X", "-Ps"}
	if line > 1 {
		argv = append(argv, fmt.Sprintf("+%d", line))
	}

	return append(argv, path)
}
