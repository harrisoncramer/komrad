package diffview

import (
	"fmt"
	"image/color"
	"slices"
	"strings"

	"komrad/internal/diff"
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/lib/xstr"
	agentv1 "komrad/proto/gen/agent/v1"

	"charm.land/lipgloss/v2"
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

var (
	addedBackground     = paint.Adaptive{Light: lipgloss.Color("#dafbe1"), Dark: lipgloss.Color("#1b3823")}
	deletedBackground   = paint.Adaptive{Light: lipgloss.Color("#ffebe9"), Dark: lipgloss.Color("#402222")}
	cursorBackground    = paint.Adaptive{Light: lipgloss.Color("#dbeafe"), Dark: lipgloss.Color("#1e3a5f")}
	dimCursorBackground = paint.Adaptive{Light: lipgloss.Color("#e5e7eb"), Dark: lipgloss.Color("#2b3038")}
	matchBackground     = paint.Adaptive{Light: lipgloss.Color("#fde68a"), Dark: lipgloss.Color("#7c5c00")}
	plainBackground     = lipgloss.NoColor{}
	gutterStyle         = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	fileStyle           = lipgloss.NewStyle().Bold(true)
	metadataStyle       = lipgloss.NewStyle().Faint(true)
	hunkStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	agentMarkStyle      = paint.Panel.Style(paint.ToneOther)
	humanMarkStyle      = paint.Panel.Style(paint.ToneSelf)
	offerMarkStyle      = paint.Panel.Style(paint.ToneWarning)
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
	case changeRow:
		return v.renderChangeRow(current, cursor, width)
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
		text += style.Render("  " + v.glyphs.Spell(glyph.FileViewed) + " viewed  " + v.fileSummary(current.file))
	}
	var offered string
	if offers := v.GetOffersAt(y); len(offers) > 0 {
		mark, markStyle := v.offerMarkFor(offers)
		offered = style.Render("  ") + markStyle.Background(background).Render(fmt.Sprintf("%s %d", mark, len(offers)))
	}

	text = ansi.Truncate(ansi.Truncate(text, max(width-lipgloss.Width(offered), 0), "…")+offered, width, "…")
	if padding := width - lipgloss.Width(text); cursor != NoCursor && padding > 0 {
		text += style.Render(strings.Repeat(" ", padding))
	}

	return text
}

// fileSummary is how much a collapsed file is hiding: the lines it changed, and how many
// conversations in it are still open.
func (v *View) fileSummary(fileIndex int) string {
	stat := v.files[fileIndex].Stat()

	summary := fmt.Sprintf("+%d −%d", stat.Added, stat.Removed)
	if open := v.countOpenThreadsIn(v.files[fileIndex].Path()); open > 0 {
		summary += fmt.Sprintf("  %s %d", v.glyphs.Spell(glyph.ThreadRead), open)
	}

	return summary
}

// countOpenThreadsIn is how many conversations in a file are still owed an answer.
func (v *View) countOpenThreadsIn(path string) int {
	open := 0
	for _, thread := range v.threads {
		if thread.GetFilePath() == path && isUnsettled(thread) {
			open++
		}
	}

	return open
}

// renderGutter draws the line numbers, what the diff did to the line, and whether a thread is
// pinned to it.
func (v *View) renderGutter(y int, line diff.Line, background color.Color) string {
	mark, markStyle := " ", gutterStyle
	if thread, ok := v.GetThreadAt(y); ok {
		mark, markStyle = v.markFor(thread)
	} else if offers := v.GetOffersAt(y); len(offers) > 0 {
		mark, markStyle = v.offerMarkFor(offers)
	}

	numbers := fmt.Sprintf("%4s %4s %s",
		formatNumber(line.OldNumber), formatNumber(line.NewNumber), markerFor(line.Kind))

	return gutterStyle.Background(background).Render(numbers) +
		markStyle.Background(background).Render(mark) +
		gutterStyle.Background(background).Render(" ")
}

// markFor is the glyph a thread gets in the gutter, drawn in the colour of whoever opened it.
func (v *View) markFor(thread *agentv1.ReviewThread) (string, lipgloss.Style) {
	style := markStyleFor(thread)

	switch {
	case doesHoldDelivery(thread, agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT):
		return v.glyphs.Spell(glyph.ThreadUnsent), style
	case thread.GetUnreadByHuman():
		return v.glyphs.Spell(glyph.ThreadUnread), style
	case thread.GetResolvedAt() != "":
		return v.glyphs.Spell(glyph.ThreadSettled), style
	}

	return v.glyphs.Spell(glyph.ThreadRead), style
}

// markStyleFor is the colour a thread is drawn in, which says who opened it.
func markStyleFor(thread *agentv1.ReviewThread) lipgloss.Style {
	if isAgentThread(thread) {
		return agentMarkStyle
	}

	return humanMarkStyle
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
	mark, markStyle := v.markFor(current.thread)

	said := FormatThreadLocation(current.thread)
	if opening := firstLine(current.thread); opening != "" {
		said += "  " + opening
	}

	return renderSaidRow(renderSaidRowParams{
		Mark:      mark,
		MarkStyle: markStyle,
		Said:      said,
		Cursor:    cursor,
		Width:     width,
	})
}

// renderChangeRow draws the row above the first file, saying how many comments on the change as a
// whole wait at the foot.
func (v *View) renderChangeRow(current row, cursor Cursor, width int) string {
	said := current.text
	if count := v.countChangeThreads(); count > 0 {
		said += fmt.Sprintf("  %d at the end of the diff", count)
	}

	return renderSaidRow(renderSaidRowParams{
		Mark:      " ",
		MarkStyle: gutterStyle,
		Said:      said,
		Cursor:    cursor,
		Width:     width,
	})
}

// countChangeThreads counts the comments on the change as a whole.
func (v *View) countChangeThreads() int {
	count := 0
	for _, thread := range v.threads {
		if thread.GetFilePath() == "" {
			count++
		}
	}

	return count
}

// renderSaidRowParams is a row of prose drawn in the diff and the mark beside it.
type renderSaidRowParams struct {
	Mark      string
	MarkStyle lipgloss.Style
	Said      string
	Cursor    Cursor
	Width     int
}

// renderSaidRow draws a row of prose with nothing but a thread's mark in its gutter.
func renderSaidRow(params renderSaidRowParams) string {
	background := getBackground(diff.Context, params.Cursor)
	style := lipgloss.NewStyle().Background(background)
	head := strings.Repeat(" ", max(GutterWidth-2, 0))
	said := style.Foreground(lipgloss.Color("8")).Italic(true).Render(params.Said)

	return ansi.Truncate(
		style.Render(head)+params.MarkStyle.Background(background).Render(params.Mark)+style.Render(" ")+
			fitWindow(said, 0, max(0, params.Width-GutterWidth), background),
		params.Width, "…")
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

func getBackground(kind diff.LineKind, cursor Cursor) color.Color {
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
func renderTokens(line styledLine, fallback string, background color.Color, found []span) string {
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
func fitWindow(value string, offset, width int, background color.Color) string {
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
