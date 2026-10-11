package diffview

import (
	"fmt"
	"image/color"
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

// Render is the window of rows the pane shows, with the cursor's row marked as given and blank
// lines under a view shorter than the pane.
func (p *Pane) Render(window Window, held Cursor) []string {
	offset := p.pan
	if window.Wrap {
		offset = 0
	}

	// Only the rows the pane can show are rendered, a nine thousand row diff taking a tenth of a
	// second to render whole.
	lines := make([]string, 0, window.Height)
	start := p.findWindowStart(window)
	for y := start; y < p.view.Len() && len(lines) < window.Height; y++ {
		mark := noCursor
		if y == p.cursor {
			mark = held
		}

		lines = append(lines, p.view.renderLines(y, mark, window, offset)...)
	}

	lines = lines[:min(len(lines), max(window.Height, 0))]
	p.coverWithHeader(lines, start, window)

	for len(lines) < window.Height {
		lines = append(lines, strings.Repeat(" ", window.Width))
	}

	return lines
}

// coverWithHeader renders the header of the file the window starts inside over the window's first row.
func (p *Pane) coverWithHeader(lines []string, start int, window Window) {
	header, ok := p.view.findFileHeader(start)
	if !ok || p.cursor == start || len(lines) == 0 {
		return
	}

	lines[0] = p.view.renderRow(header, noCursor, window.Width, 0)
	for i := 1; i < min(p.view.measureRow(start, window), len(lines)); i++ {
		lines[i] = strings.Repeat(" ", window.Width)
	}
}

// Cursor is how the row the reader is standing on is rendered, dim while the keys are somewhere
// else.
type Cursor int

const (
	noCursor Cursor = iota
	LiveCursor
	DimCursor
)

var (
	plainBackground = lipgloss.NoColor{}
	fileStyle       = lipgloss.NewStyle().Bold(true)
	metadataStyle   = lipgloss.NewStyle().Faint(true)
)

// renderRow renders one row of the diff at the width it is shown at, taking the cursor as an
// argument since only the row under it is rerendered as it moves.
func (v *View) renderRow(y int, cursor Cursor, width, offset int) string {
	current, ok := v.getRow(y)
	if !ok {
		return ""
	}

	switch current.kind {
	case fileRow:
		return v.renderFileRow(y, current, cursor, width)
	case metadataRow:
		return ansi.Truncate(metadataStyle.Render("  "+sanitize(current.text)), width, "…")
	case hunkRow:
		return ansi.Truncate(paint.Panel.Style(paint.ToneSecondary).Render(sanitize(current.text)), width, "…")
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

// measureRow is how many lines a row takes in the window, more than one only for a code line
// wrapped past the window's edge.
func (v *View) measureRow(y int, window Window) int {
	codeWidth := window.Width - GutterWidth
	if !window.Wrap || codeWidth <= 0 {
		return 1
	}

	current, _ := v.getRow(y)
	line, ok := v.getLine(y)
	if !ok {
		return 1
	}

	text := line.Text
	if styled := v.getStyledLine(current, line); len(styled) > 0 {
		text = styled.text()
	}

	return strings.Count(ansi.Hardwrap(sanitize(text), codeWidth, true), "\n") + 1
}

// renderLines renders a row as the lines it takes in the window, a wrapped code line carrying its
// gutter on the first and a blank one on the rest.
func (v *View) renderLines(y int, cursor Cursor, window Window, offset int) []string {
	if v.measureRow(y, window) == 1 {
		return []string{v.renderRow(y, cursor, window.Width, offset)}
	}

	current, _ := v.getRow(y)
	line, _ := v.getLine(y)
	background := getBackground(line.Kind, cursor)
	content := renderTokens(v.getStyledLine(current, line), line.Text, background, v.matchesIn(y))
	codeWidth := window.Width - GutterWidth

	pieces := carryStyle(strings.Split(ansi.Hardwrap(content, codeWidth, true), "\n"))
	blank := lipgloss.NewStyle().Background(background).Render(strings.Repeat(" ", GutterWidth))
	lines := make([]string, 0, len(pieces))
	for i, piece := range pieces {
		gutter := blank
		if i == 0 {
			gutter = v.renderGutter(y, line, background)
		}

		lines = append(lines, gutter+fitWindow(piece, 0, codeWidth, background))
	}

	return lines
}

// carryStyle opens each wrapped line in the style the line before it left open, and closes any
// line that ends with one open.
func carryStyle(lines []string) []string {
	carried := make([]string, len(lines))

	open := ""
	for i, line := range lines {
		line = open + line
		open = findOpenStyle(line)
		if open != "" {
			line += ansi.ResetStyle
		}

		carried[i] = line
	}

	return carried
}

// findOpenStyle is the run of style sequences still in force at the end of a line.
func findOpenStyle(line string) string {
	var open strings.Builder

	parser := ansi.NewParser()
	state := byte(0)
	for len(line) > 0 {
		sequence, _, read, next := ansi.DecodeSequence(line, state, parser)
		state = next
		line = line[read:]

		if !strings.HasPrefix(sequence, "\x1b[") || !strings.HasSuffix(sequence, "m") {
			continue
		}

		if sequence == ansi.ResetStyle || sequence == "\x1b[0m" {
			open.Reset()

			continue
		}

		open.WriteString(sequence)
	}

	return open.String()
}

// renderFileRow renders a file's header, which for a collapsed file is its only row and says what
// is folded under it.
func (v *View) renderFileRow(y int, current row, cursor Cursor, width int) string {
	background := getBackground(diff.Context, cursor)
	style := fileStyle.Background(background)
	if v.isCollapsed(current.file) {
		style = metadataStyle.Background(background)
	}

	var out strings.Builder
	for _, piece := range cutBy(current.text, 0, v.rawMatches(y)) {
		pieceStyle := style
		if piece.matched {
			pieceStyle = style.Background(paint.Panel.Background(paint.ShadeMatch))
		}

		out.WriteString(pieceStyle.Render(sanitize(piece.text)))
	}

	text := out.String()
	if v.isCollapsed(current.file) {
		text += style.Render("  " + v.fileSummary(current.file))
	}
	text = ansi.Truncate(text, width, "…")
	if padding := width - lipgloss.Width(text); cursor != noCursor && padding > 0 {
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

// renderGutter renders the line numbers, what the diff did to the line, and whether a thread is
// pinned to it.
func (v *View) renderGutter(y int, line diff.Line, background color.Color) string {
	mark, markStyle := " ", paint.Panel.Style(paint.ToneMuted)
	if thread, ok := v.GetThreadAt(y); ok {
		mark, markStyle = v.markFor(thread)
	}

	numbers := fmt.Sprintf("%4s %4s %s",
		formatNumber(line.OldNumber), formatNumber(line.NewNumber), markerFor(line.Kind))

	return paint.Panel.Style(paint.ToneMuted).Background(background).Render(numbers) +
		markStyle.Background(background).Render(mark) +
		paint.Panel.Style(paint.ToneMuted).Background(background).Render(" ")
}

// markFor is the glyph a thread gets in the gutter, rendered in the colour of whoever opened it.
func (v *View) markFor(thread *agentv1.ReviewThread) (string, lipgloss.Style) {
	style := markStyleFor(thread)

	switch {
	case IsHoldingUnsent(thread):
		return v.glyphs.Spell(glyph.ThreadUnsent), style
	case holdsFailedForgeReply(thread):
		return v.glyphs.Spell(glyph.ThreadUnsent), style
	case thread.GetUnreadByHuman():
		return v.glyphs.Spell(glyph.ThreadUnread), style
	case thread.GetResolvedAt() != "":
		return v.glyphs.Spell(glyph.ThreadSettled), style
	case thread.GetMirrorsForge():
		return v.glyphs.Spell(glyph.ThreadForge), style
	}

	return v.glyphs.Spell(glyph.ThreadRead), style
}

// markStyleFor is the colour a thread is rendered in, which says who opened it.
func markStyleFor(thread *agentv1.ReviewThread) lipgloss.Style {
	if thread.GetMirrorsForge() {
		return paint.Panel.Style(ForgeTone)
	}
	if IsAgentThread(thread) {
		return paint.Panel.Style(paint.ToneUserAgent)
	}

	return paint.Panel.Style(paint.ToneUserMe)
}

// renderThreadRow renders a thread with no line left to stand on, saying where it was written and
// what it opened with.
func (v *View) renderThreadRow(current row, cursor Cursor, width int) string {
	mark, markStyle := v.markFor(current.thread)

	said := FormatThreadLocation(current.thread)
	if opening := firstLine(current.thread); opening != "" {
		said += "  " + opening
	}

	background := getBackground(diff.Context, cursor)
	style := lipgloss.NewStyle().Background(background)
	head := strings.Repeat(" ", max(GutterWidth-2, 0))
	rendered := style.Foreground(paint.Panel.Color(paint.ToneMuted)).Italic(true).Render(sanitize(said))

	return ansi.Truncate(
		style.Render(head)+markStyle.Background(background).Render(mark)+style.Render(" ")+
			fitWindow(rendered, 0, max(0, width-GutterWidth), background),
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

func getBackground(kind diff.LineKind, cursor Cursor) color.Color {
	switch cursor {
	case LiveCursor:
		return paint.Panel.Background(paint.ShadeSelection)
	case DimCursor:
		return paint.Panel.Background(paint.ShadeSelectionDim)
	case noCursor:
	}

	switch kind {
	case diff.Addition:
		return paint.Panel.Background(paint.ShadeDiffAdded)
	case diff.Deletion:
		return paint.Panel.Background(paint.ShadeDiffDeleted)
	case diff.Context:
	}

	return plainBackground
}

// renderTokens renders one lexed line on a background, one token at a time rather than through a
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
		// its pieces rendered separately.
		for _, piece := range cutBy(current.text, at, found) {
			pieceStyle := style
			if piece.matched {
				pieceStyle = style.Background(paint.Panel.Background(paint.ShadeMatch))
			}

			out.WriteString(pieceStyle.Render(sanitize(piece.text)))
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

func sanitize(value string) string {
	return strings.ReplaceAll(paint.StripControls(value), "\t", strings.Repeat(" ", tabWidth))
}
