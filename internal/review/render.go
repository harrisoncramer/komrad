package review

import (
	"fmt"
	"strings"

	"weave/internal/diff"
	"weave/lib/xstr"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// GutterWidth is the two line numbers, the marker, the thread mark, and the spaces between.
const GutterWidth = 13

const tabWidth = 4

const (
	threadMark = "●"
	unreadMark = "◆"
	// ghostMark is a thread whose line has changed under it, hollow so the reader does not take
	// the line beside it for the line the thread is about.
	ghostMark = "○"
	// settledMark is a thread the person reviewing has finished with, which is the only thing
	// that quiets one down.
	settledMark = "✓"
)

var (
	addedBackground = lipgloss.AdaptiveColor{Light: "#dafbe1", Dark: "#1b3823"}

	deletedBackground = lipgloss.AdaptiveColor{Light: "#ffebe9", Dark: "#402222"}

	cursorBackground = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1e3a5f"}

	matchBackground = lipgloss.AdaptiveColor{Light: "#fde68a", Dark: "#7c5c00"}

	plainBackground = lipgloss.NoColor{}

	gutterStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	fileStyle = lipgloss.NewStyle().Bold(true)

	metadataStyle = lipgloss.NewStyle().Faint(true)

	hunkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))

	threadMarkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	settledMarkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
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
	case threadRow:
		return v.renderThreadRow(current, isCursor, width)
	case lineRow:
	}

	line, ok := v.getLine(y)
	if !ok {
		return ""
	}

	background := getBackground(line.Kind, isCursor)
	content := renderTokens(v.getStyledLine(current, line), line.Text, background, v.matchesIn(y))

	return v.renderGutter(y, line, background) +
		fitWindow(content, offset, max(0, width-GutterWidth), background)
}

// renderGutter draws the line numbers, what the diff did to the line, and whether a thread is
// pinned to it. A thread the agent has replied to since you last looked is marked differently
// from one you are still waiting on.
func (v *View) renderGutter(y int, line diff.Line, background lipgloss.TerminalColor) string {
	mark, markStyle := " ", threadMarkStyle
	if thread, ok := v.ThreadAt(y); ok {
		mark, markStyle = v.markFor(thread)
	}

	numbers := fmt.Sprintf("%4s %4s %s",
		formatNumber(line.OldNumber), formatNumber(line.NewNumber), markerFor(line.Kind))

	return gutterStyle.Background(background).Render(numbers) +
		markStyle.Background(background).Render(mark) +
		gutterStyle.Background(background).Render(" ")
}

// markFor is the glyph a thread gets in the gutter. A reply you have not read outranks
// everything, then whether the thread is settled, and last whether the line under it has
// changed. A thread in the section at the foot needs none of that third one: the header there
// already says its code is gone.
func (v *View) markFor(thread *agentv1.ReviewThread) (string, lipgloss.Style) {
	switch {
	case thread.GetUnreadByHuman():
		return unreadMark, threadMarkStyle
	case thread.GetResolvedAt() != "":
		return settledMark, settledMarkStyle
	case v.IsGhosted(thread):
		return ghostMark, threadMarkStyle
	}

	return threadMark, threadMarkStyle
}

// renderThreadRow draws a thread that has no line left to stand on, in the section at the foot
// of the diff. It says where it was written and what it opened with, which is enough to know
// whether it is worth standing on to read the rest.
func (v *View) renderThreadRow(current row, isCursor bool, width int) string {
	background := getBackground(diff.Context, isCursor)
	style := lipgloss.NewStyle().Background(background)

	mark, markStyle := v.markFor(current.thread)

	head := strings.Repeat(" ", max(GutterWidth-2, 0))
	said := WhereOf(current.thread)
	if opening := firstLine(current.thread); opening != "" {
		said += "  " + opening
	}

	return style.Render(head) + markStyle.Background(background).Render(mark) + style.Render(" ") +
		fitWindow(style.Foreground(lipgloss.Color("8")).Italic(true).Render(said), 0, max(0, width-GutterWidth), background)
}

// firstLine is the opening line of a thread, which is often enough what it is about.
func firstLine(thread *agentv1.ReviewThread) string {
	messages := thread.GetMessages()
	if len(messages) == 0 {
		return ""
	}

	return xstr.FirstLine(messages[0].GetBody())
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
//
// Anything the search found is painted on its own background, so a token is cut at the edges
// of a match and its pieces drawn separately. The lexer's colours are kept either way: a match
// changes what is behind the text, never the text's own colour.
func renderTokens(line styledLine, fallback string, background lipgloss.TerminalColor, found []span) string {
	if len(line) == 0 {
		line = styledLine{{text: fallback}}
	}

	var out strings.Builder

	at := 0
	for _, current := range line {
		style := lipgloss.NewStyle().Background(background)
		if current.foreground != "" {
			style = style.Foreground(lipgloss.Color(current.foreground))
		}

		for _, piece := range cutBy(current.text, at, found) {
			pieceStyle := style
			if piece.matched {
				pieceStyle = style.Background(matchBackground)
			}

			out.WriteString(pieceStyle.Render(expandTabs(piece.text)))
		}

		at += len(current.text)
	}

	return out.String()
}

// piece is a run of one token that is either all inside a match or all outside one.
type piece struct {
	text    string
	matched bool
}

// cutBy splits a token at every edge of a match that falls inside it. The token's own offset
// in the line is passed in, since a match is recorded against the line rather than the token.
func cutBy(text string, at int, found []span) []piece {
	if len(found) == 0 {
		return []piece{{text: text}}
	}

	var pieces []piece

	start := 0
	for offset := range len(text) + 1 {
		if offset == len(text) || inSpan(at+offset, found) != inSpan(at+start, found) {
			if offset > start {
				pieces = append(pieces, piece{text: text[start:offset], matched: inSpan(at+start, found)})
			}

			start = offset
		}
	}

	return pieces
}

func inSpan(offset int, found []span) bool {
	for _, at := range found {
		if offset >= at.start && offset < at.end {
			return true
		}
	}

	return false
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
