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

func pagerArgv(path string, line int) []string {
	argv := []string{"less", "-R", "-X", "-Ps"}
	if line > 1 {
		argv = append(argv, fmt.Sprintf("+%d", line))
	}

	return append(argv, path)
}
