package review

import (
	"fmt"
	"strings"

	"weave/internal/diff"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// gutterWidth is the two line numbers, the marker, and the spaces between them.
const gutterWidth = 12

const tabWidth = 4

const horizontalStep = 8

var (
	addedBackground = lipgloss.AdaptiveColor{Light: "#dafbe1", Dark: "#1b3823"}

	deletedBackground = lipgloss.AdaptiveColor{Light: "#ffebe9", Dark: "#402222"}

	cursorBackground = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1e3a5f"}

	plainBackground = lipgloss.NoColor{}

	gutterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fileStyle = lipgloss.NewStyle().Bold(true)

	metadataStyle = lipgloss.NewStyle().Faint(true)

	hunkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))

	headerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)

	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	errorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
)

// View draws the header, the window of rows around the cursor, and the footer, which reports
// the anchor under the cursor so the seam the rest of this feature hangs off is visible.
func (m model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	body := m.getBodyHeight()

	var out strings.Builder
	out.WriteString(m.renderHeader() + "\n")
	for y := m.top; y < m.top+body && y < len(m.view.rows); y++ {
		out.WriteString(m.renderRow(y) + "\n")
	}
	for range body - min(body, len(m.view.rows)-m.top) {
		out.WriteString("\n")
	}
	out.WriteString(m.renderFooter())

	return out.String()
}

func (m model) renderHeader() string {
	path := "no changes"
	if file, ok := m.view.getFile(m.cursor); ok {
		path = file.Path()
	}

	base := fmt.Sprintf("  %s (%s)", m.patch.Base, shortCommit(m.patch.Commit))

	return ansi.Truncate(headerStyle.Render(path)+gutterStyle.Render(base), m.width, "…")
}

func (m model) renderFooter() string {
	if m.err != nil {
		return ansi.Truncate(errorStyle.Render(m.err.Error()), m.width, "…")
	}

	anchor := "—"
	if current, ok := m.view.GetAnchor(m.cursor); ok {
		anchor = describeAnchor(*current)
	}

	keys := "  j/k move  ]/[ file  h/l pan  q quit"

	return ansi.Truncate(footerStyle.Render(anchor+keys), m.width, "…")
}

func describeAnchor(anchor Anchor) string {
	side := fmt.Sprintf("new %d", anchor.NewStart)
	if anchor.NewStart == 0 {
		side = fmt.Sprintf("old %d", anchor.OldStart)
	}

	return fmt.Sprintf("%s:%s", anchor.FilePath, side)
}

func (m model) renderRow(y int) string {
	current, ok := m.view.getRow(y)
	if !ok {
		return ""
	}

	isCursor := y == m.cursor

	switch current.kind {
	case fileRow:
		return ansi.Truncate(fileStyle.Render(current.text), m.width, "…")
	case metadataRow:
		return ansi.Truncate(metadataStyle.Render("  "+current.text), m.width, "…")
	case hunkRow:
		return ansi.Truncate(hunkStyle.Render(current.text), m.width, "…")
	case lineRow:
	}

	line, ok := m.view.getLine(y)
	if !ok {
		return ""
	}

	background := getBackground(line.Kind, isCursor)
	content := renderTokens(m.view.getStyledLine(current, line), line.Text, background)

	return m.renderGutter(line, background) +
		fitWindow(content, m.leftColumn, max(0, m.width-gutterWidth), background)
}

func (m model) renderGutter(line diff.Line, background lipgloss.TerminalColor) string {
	numbers := fmt.Sprintf("%4s %4s %s ",
		formatNumber(line.OldNumber), formatNumber(line.NewNumber), markerFor(line.Kind))

	return gutterStyle.Background(background).Render(numbers)
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

func shortCommit(commit string) string {
	if len(commit) > 8 {
		return commit[:8]
	}

	return commit
}
