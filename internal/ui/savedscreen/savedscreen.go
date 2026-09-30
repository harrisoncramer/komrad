// Package savedscreen fits a captured pane screen to a terminal of another size.
package savedscreen

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// tabStop is how far apart tmux sets a pane's tab stops.
const tabStop = 8

// Fit is screen drawn width by height, cut at the right, with its rows cut from the longest
// blank run and then the top when it is too tall.
func Fit(screen string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	lines := strings.Split(strings.TrimSuffix(screen, "\n"), "\n")
	for i, line := range lines {
		lines[i] = ExpandTabs(line)
	}

	lines = shorten(lines, height)

	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(lipgloss.NewCompositor(lipgloss.NewLayer(strings.Join(lines, "\n"))))

	return canvas.Render()
}

// shorten cuts lines to height, collapsing the longest run of blank rows to one before cutting
// from the top.
func shorten(lines []string, height int) []string {
	excess := len(lines) - height
	if excess <= 0 {
		return lines
	}

	start, run := findLongestBlankRun(lines)
	if cut := min(excess, run-1); cut > 0 {
		lines = append(lines[:start+1:start+1], lines[start+1+cut:]...)
		excess -= cut
	}

	if excess > 0 {
		lines = lines[excess:]
	}

	return lines
}

func findLongestBlankRun(lines []string) (start, length int) {
	current := 0
	for i, line := range lines {
		if strings.TrimSpace(ansi.Strip(line)) != "" {
			current = 0

			continue
		}

		current++
		if current > length {
			start, length = i-current+1, current
		}
	}

	return start, length
}

// ExpandTabs spends a line's tabs as the spaces tmux moved the cursor over.
func ExpandTabs(line string) string {
	if !strings.Contains(line, "\t") {
		return line
	}

	var b strings.Builder

	col := 0
	for i, piece := range strings.Split(line, "\t") {
		if i > 0 {
			pad := tabStop - col%tabStop
			b.WriteString(strings.Repeat(" ", pad))
			col += pad
		}

		b.WriteString(piece)
		col += lipgloss.Width(piece)
	}

	return b.String()
}
