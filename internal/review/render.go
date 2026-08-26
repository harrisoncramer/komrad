package review

import (
	"fmt"
	"strings"

	"weave/internal/diff"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// GutterWidth is the two line numbers, the marker, the thread mark, and the spaces between.
const GutterWidth = 13

const tabWidth = 4

const (
	threadMark = "●"
	unreadMark = "◆"
)

var (
	addedBackground = lipgloss.AdaptiveColor{Light: "#dafbe1", Dark: "#1b3823"}

	deletedBackground = lipgloss.AdaptiveColor{Light: "#ffebe9", Dark: "#402222"}

	cursorBackground = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1e3a5f"}

	plainBackground = lipgloss.NoColor{}

	gutterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fileStyle = lipgloss.NewStyle().Bold(true)

	metadataStyle = lipgloss.NewStyle().Faint(true)

	hunkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))

	threadMarkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
)

// RenderRow draws one row of the diff at the width it is being shown at. The cursor is passed
// in rather than held, since the row list is drawn once and only the row under the cursor is
// redrawn as it moves.
func (v *View) RenderRow(y int, isCursor bool, width, offset int) string {
	current, ok := v.getRow(y)
	if !ok {
		return ""
	}

	switch current.kind {
	case fileRow:
		return ansi.Truncate(fileStyle.Render(current.text), width, "…")
	case metadataRow:
		return ansi.Truncate(metadataStyle.Render("  "+current.text), width, "…")
	case hunkRow:
		return ansi.Truncate(hunkStyle.Render(current.text), width, "…")
	case lineRow:
	}

	line, ok := v.getLine(y)
	if !ok {
		return ""
	}

	background := getBackground(line.Kind, isCursor)
	content := renderTokens(v.getStyledLine(current, line), line.Text, background)

	return v.renderGutter(y, line, background) +
		fitWindow(content, offset, max(0, width-GutterWidth), background)
}

// renderGutter draws the line numbers, what the diff did to the line, and whether a thread is
// pinned to it. A thread the agent has replied to since you last looked is marked differently
// from one you are still waiting on.
func (v *View) renderGutter(y int, line diff.Line, background lipgloss.TerminalColor) string {
	mark := " "
	if thread, ok := v.ThreadAt(y); ok {
		mark = threadMark
		if thread.GetUnreadByHuman() {
			mark = unreadMark
		}
	}

	numbers := fmt.Sprintf("%4s %4s %s",
		formatNumber(line.OldNumber), formatNumber(line.NewNumber), markerFor(line.Kind))

	return gutterStyle.Background(background).Render(numbers) +
		threadMarkStyle.Background(background).Render(mark) +
		gutterStyle.Background(background).Render(" ")
}

func formatNumber(value int) string {
	if value == 0 {
		return ""
	}

	return fmt.Sprint(value)
}

func getBackground(kind diff.LineKind, isCursor bool) lipgloss.TerminalColor {
	switch {
	case isCursor:
		return cursorBackground
	case kind == diff.Addition:
		return addedBackground
	case kind == diff.Deletion:
		return deletedBackground
	}

	return plainBackground
}

// renderTokens draws one lexed line on a background. The tokens are drawn one at a time
// rather than through a chroma formatter, which is what lets the background survive: a
// formatter resets after every token, and a reset drops whatever was painted behind it.
func renderTokens(line styledLine, fallback string, background lipgloss.TerminalColor) string {
	if len(line) == 0 {
		return lipgloss.NewStyle().Background(background).Render(expandTabs(fallback))
	}

	var out strings.Builder
	for _, current := range line {
		style := lipgloss.NewStyle().Background(background)
		if current.foreground != "" {
			style = style.Foreground(lipgloss.Color(current.foreground))
		}

		out.WriteString(style.Render(expandTabs(current.text)))
	}

	return out.String()
}

// fitWindow slides an already-coloured line under the horizontal offset and pads it out to
// the full width, so the background reaches the edge of the pane rather than stopping at the
// end of the text.
func fitWindow(value string, offset, width int, background lipgloss.TerminalColor) string {
	if width <= 0 {
		return ""
	}

	if offset > 0 {
		value = ansi.TruncateLeft(value, offset, "")
	}
	value = ansi.Truncate(value, width, "")

	if padding := width - lipgloss.Width(value); padding > 0 {
		value += lipgloss.NewStyle().Background(background).Render(strings.Repeat(" ", padding))
	}

	return value
}

func expandTabs(value string) string {
	return strings.ReplaceAll(value, "\t", strings.Repeat(" ", tabWidth))
}
