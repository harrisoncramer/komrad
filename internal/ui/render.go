package ui

import (
	"fmt"
	"os"
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

	return rendered
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
