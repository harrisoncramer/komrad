package review

import (
	"fmt"
	"slices"
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

// Cursor is how the row the reader is standing on is drawn, dim while the keys are somewhere
// else.
type Cursor int

const (
	NoCursor Cursor = iota
	LiveCursor
	DimCursor
)

const (
	threadMark = "●"
	unreadMark = "◆"
	// ghostMark is a thread whose line has changed under it, drawn hollow.
	ghostMark = "○"
	// settledMark is a thread the person reviewing has finished with.
	settledMark = "✓"
	// unsentMark is a thread holding a comment nobody has released yet, drawn hollow against the
	// filled mark of a reply waiting to be read.
	unsentMark = "◇"
	// viewedMark heads the header of a file that has been read and folded away.
	viewedMark = "✓"
)

var (
	addedBackground     = lipgloss.AdaptiveColor{Light: "#dafbe1", Dark: "#1b3823"}
	deletedBackground   = lipgloss.AdaptiveColor{Light: "#ffebe9", Dark: "#402222"}
	cursorBackground    = lipgloss.AdaptiveColor{Light: "#dbeafe", Dark: "#1e3a5f"}
	dimCursorBackground = lipgloss.AdaptiveColor{Light: "#e5e7eb", Dark: "#2b3038"}
	matchBackground     = lipgloss.AdaptiveColor{Light: "#fde68a", Dark: "#7c5c00"}
	plainBackground     = lipgloss.NoColor{}
	gutterStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	fileStyle           = lipgloss.NewStyle().Bold(true)
	metadataStyle       = lipgloss.NewStyle().Faint(true)
	hunkStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	threadMarkStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	settledMarkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	unsentMarkStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
)

// RenderRow draws one row of the diff at the width it is shown at, taking the cursor as an
// argument since only the row under it is redrawn as it moves.
func (v *View) RenderRow(y int, cursor Cursor, width, offset int) string {
	current, ok := v.getRow(y)
	if !ok {
		return ""
	}

	switch current.kind {
	case fileRow:
		return v.renderFileRow(y, current, cursor, width)
	case metadataRow:
		return ansi.Truncate(metadataStyle.Render("  "+current.text), width, "…")
	case hunkRow:
		return ansi.Truncate(hunkStyle.Render(current.text), width, "…")
	case threadRow:
		return v.renderThreadRow(current, cursor, width)
	case lineRow:
	}

	line, ok := v.getLine(y)
	if !ok {
		return ""
	}

	background := getBackground(line.Kind, cursor)
	content := renderTokens(v.getStyledLine(current, line), line.Text, background, v.matchesIn(y))

	return ansi.Truncate(
		v.renderGutter(y, line, background)+fitWindow(content, offset, max(0, width-GutterWidth), background),
		width, "…")
}

// renderFileRow draws a file's header, which for a collapsed file is its only row and says what
// is folded under it.
func (v *View) renderFileRow(y int, current row, cursor Cursor, width int) string {
	background := getBackground(diff.Context, cursor)
	style := fileStyle.Background(background)

	var out strings.Builder
	for _, piece := range cutBy(current.text, 0, v.rawMatches(y)) {
		pieceStyle := style
		if piece.matched {
			pieceStyle = style.Background(matchBackground)
		}

		out.WriteString(pieceStyle.Render(piece.text))
	}

	text := out.String()
	if v.isCollapsed(current.file) {
		text += style.Render("  " + viewedMark + " viewed  " + v.fileSummary(current.file))
	}

	text = ansi.Truncate(text, width, "…")
	if padding := width - lipgloss.Width(text); cursor != NoCursor && padding > 0 {
		text += style.Render(strings.Repeat(" ", padding))
	}

	return text
}

// fileSummary is how much a collapsed file is hiding: the lines it changed, and how many
// conversations in it are still open.
func (v *View) fileSummary(fileIndex int) string {
	stat := v.patch.Files[fileIndex].Stat()

	summary := fmt.Sprintf("+%d −%d", stat.Added, stat.Removed)
	if open := v.countOpenThreadsIn(v.patch.Files[fileIndex].Path()); open > 0 {
		summary += fmt.Sprintf("  %s %d", threadMark, open)
	}

	return summary
}

// countOpenThreadsIn is how many conversations in a file are still owed an answer.
func (v *View) countOpenThreadsIn(path string) int {
	open := 0
	for _, thread := range v.threads {
		if thread.GetFilePath() == path && thread.GetResolvedAt() == "" {
			open++
		}
	}

	return open
}

// renderGutter draws the line numbers, what the diff did to the line, and whether a thread is
// pinned to it.
func (v *View) renderGutter(y int, line diff.Line, background lipgloss.TerminalColor) string {
	mark, markStyle := " ", threadMarkStyle
	if thread, ok := v.GetThreadAt(y); ok {
		mark, markStyle = v.markFor(thread)
	}

	numbers := fmt.Sprintf("%4s %4s %s",
		formatNumber(line.OldNumber), formatNumber(line.NewNumber), markerFor(line.Kind))

	return gutterStyle.Background(background).Render(numbers) +
		markStyle.Background(background).Render(mark) +
		gutterStyle.Background(background).Render(" ")
}

// markFor is the glyph a thread gets in the gutter, ranking an unread reply first, then a
// settled thread, then an unreleased comment, and last a line that has changed under it.
func (v *View) markFor(thread *agentv1.ReviewThread) (string, lipgloss.Style) {
	switch {
	case thread.GetUnreadByHuman():
		return unreadMark, threadMarkStyle
	case thread.GetResolvedAt() != "":
		return settledMark, settledMarkStyle
	case doesHoldDelivery(thread, agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT):
		return unsentMark, unsentMarkStyle
	case v.IsGhosted(thread):
		return ghostMark, threadMarkStyle
	}

	return threadMark, threadMarkStyle
}

// doesHoldDelivery reports whether any comment in a thread stands in one delivery state.
func doesHoldDelivery(thread *agentv1.ReviewThread, state agentv1.ReviewDeliveryState) bool {
	return slices.ContainsFunc(thread.GetMessages(), func(message *agentv1.ReviewMessage) bool {
		return message.GetDelivery() == state
	})
}

// renderThreadRow draws a thread with no line left to stand on, saying where it was written and
// what it opened with.
func (v *View) renderThreadRow(current row, cursor Cursor, width int) string {
	background := getBackground(diff.Context, cursor)
	style := lipgloss.NewStyle().Background(background)

	mark, markStyle := v.markFor(current.thread)

	head := strings.Repeat(" ", max(GutterWidth-2, 0))
	said := FormatThreadLocation(current.thread)
	if opening := firstLine(current.thread); opening != "" {
		said += "  " + opening
	}

	return ansi.Truncate(
		style.Render(head)+markStyle.Background(background).Render(mark)+style.Render(" ")+
			fitWindow(style.Foreground(lipgloss.Color("8")).Italic(true).Render(said), 0, max(0, width-GutterWidth), background),
		width, "…")
}

// firstLine is the opening line of a thread.
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

func getBackground(kind diff.LineKind, cursor Cursor) lipgloss.TerminalColor {
	switch cursor {
	case LiveCursor:
		return cursorBackground
	case DimCursor:
		return dimCursorBackground
	case NoCursor:
	}

	switch kind {
	case diff.Addition:
		return addedBackground
	case diff.Deletion:
		return deletedBackground
	case diff.Context:
	}

	return plainBackground
}

// renderTokens draws one lexed line on a background, one token at a time rather than through a
// chroma formatter, which resets after every token and loses the background.
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

		// A match is painted on its own background, so a token is cut at the edges of one and
		// its pieces drawn separately.
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

// cutBy splits a token at every edge of a match inside it, taking the token's offset in the
// line since a match is recorded against the line.
func cutBy(text string, at int, found []span) []piece {
	if len(found) == 0 {
		return []piece{{text: text}}
	}

	var pieces []piece

	start := 0
	for offset := range len(text) + 1 {
		if offset == len(text) || isInSpan(at+offset, found) != isInSpan(at+start, found) {
			if offset > start {
				pieces = append(pieces, piece{text: text[start:offset], matched: isInSpan(at+start, found)})
			}

			start = offset
		}
	}

	return pieces
}

func isInSpan(offset int, found []span) bool {
	for _, at := range found {
		if offset >= at.start && offset < at.end {
			return true
		}
	}

	return false
}

// fitWindow slides an already-coloured line under the horizontal offset and pads it out to the
// full width, so the background reaches the edge of the pane.
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
