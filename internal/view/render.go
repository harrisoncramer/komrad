package view

import (
	"fmt"
	"strings"

	"weave/internal/ui/paint"

	"github.com/charmbracelet/lipgloss"
)

// headerGutter is the focus and mark cells the column header has nothing to put in.
const headerGutter = "    "

// Header is the row of column names standing over the list, greyed throughout.
func Header(palette paint.Palette) string {
	cells := []Cell{
		{Glyph: GlyphRepo, Tone: paint.ToneMuted},
		{Glyph: GlyphReview, Tone: paint.ToneMuted},
		{Glyph: GlyphForge, Tone: paint.ToneMuted},
	}

	drawn := make([]string, 0, len(cells))
	for _, cell := range cells {
		drawn = append(drawn, drawCell(palette, cell))
	}

	return headerGutter + strings.Join(drawn, " ")
}

// Render draws every row of the agents list, one to a line.
func Render(list List, palette paint.Palette, width int) string {
	return strings.Join(Rows(list, palette, width), "\n")
}

// Rows draws every row of the agents list as its own string, which is what a caller that pads
// or scrolls the list one row at a time takes.
func Rows(list List, palette paint.Palette, width int) []string {
	rows := make([]string, 0, len(list.Lines))
	for _, line := range list.Lines {
		rows = append(rows, renderLine(line, palette, width))
	}

	return rows
}

// renderLine spells one prepared row out, painting the selection bar under it last so the
// colours it already carries read through.
func renderLine(line Line, palette paint.Palette, width int) string {
	gutter := strings.Join([]string{
		drawGutterCell(palette, line.Focus),
		drawGutterCell(palette, line.Mark),
		drawCell(palette, line.Repo),
		drawCell(palette, line.Review),
		drawCell(palette, line.Forge),
		drawGutterCell(palette, line.Status),
	}, " ")

	row := gutter + " " + drawRule(palette, line.Rule) +
		drawName(palette, line) + drawTrailing(palette, line)

	if line.Selected {
		return paint.PaintRow(row, width-extraColumns(line))
	}

	return row
}

// drawName is a name in its own lettering, with whatever a search picked out of it drawn in the
// match colour instead.
func drawName(palette paint.Palette, line Line) string {
	base := nameStyle(palette, line.Weight, line.Emphasis)

	var b strings.Builder
	at := 0
	for _, match := range line.Matches {
		if match.Start < at || match.End > len(line.Name) {
			continue
		}

		writeStyled(&b, base, line.Name[at:match.Start])
		writeStyled(&b, palette.Match(), line.Name[match.Start:match.End])
		at = match.End
	}

	writeStyled(&b, base, line.Name[at:])

	return b.String()
}

// writeStyled adds a stretch of a name in the style it is drawn in, an empty one carrying no
// escapes of its own.
func writeStyled(b *strings.Builder, style lipgloss.Style, segment string) {
	if segment == "" {
		return
	}

	b.WriteString(style.Render(segment))
}

// drawTrailing is the odds and ends after a name, each one a space away from what precedes it.
func drawTrailing(palette paint.Palette, line Line) string {
	var b strings.Builder
	for _, cell := range []Cell{line.Held, line.Mail, line.Alert} {
		if !cell.IsBlank() {
			b.WriteString(" " + drawCell(palette, cell))
		}
	}

	if line.Tally > 0 {
		b.WriteString(" " + palette.Style(paint.ToneMuted).Render(fmt.Sprintf("+%d", line.Tally)))
	}

	if !line.Schedule.IsBlank() {
		b.WriteString(" " + drawCell(palette, line.Schedule))
	}

	return b.String()
}

// drawGutterCell is a cell that holds its column whether or not it has anything to draw.
func drawGutterCell(palette paint.Palette, cell Cell) string {
	if cell.IsBlank() {
		return " "
	}

	return drawCell(palette, cell)
}

// drawRule is the corner a child hangs off its parent by.
func drawRule(palette paint.Palette, rule bool) string {
	if !rule {
		return ""
	}

	return palette.Style(paint.ToneMuted).Render(spell(GlyphChild)) + " "
}

// drawCell is what a cell holds, in its own tone.
func drawCell(palette paint.Palette, cell Cell) string {
	text := cell.Text
	if text == "" {
		text = spell(cell.Glyph)
	}

	return palette.Style(cell.Tone).Render(text)
}

// nameStyle is the lettering a name is drawn in: its weight from where it sits in the tree, its
// foreground from what it has to say.
func nameStyle(palette paint.Palette, weight Weight, emphasis Emphasis) lipgloss.Style {
	style := lipgloss.NewStyle()

	switch emphasis {
	case EmphasisNormal:
	case EmphasisBright:
		style = style.Foreground(palette.Color(paint.ToneBright))
	case EmphasisDim:
		style = style.Foreground(palette.Color(paint.ToneMuted))
	case EmphasisFocused:
		style = style.Foreground(palette.Color(paint.ToneAccent))
	}

	switch weight {
	case WeightRegular:
	case WeightBold:
		style = style.Bold(true)
	case WeightItalic:
		style = style.Italic(true)
	}

	return style
}

// spell is the one place a glyph is written out, every one of them a Nerd Font Octicon or a
// box-drawing rule.
func spell(glyph Glyph) string {
	switch glyph {
	case GlyphNone:
		return ""
	case GlyphRepo:
		return ""
	case GlyphReview:
		return ""
	case GlyphForge:
		return ""
	case GlyphApproved:
		return ""
	case GlyphBroken:
		return ""
	case GlyphChanges:
		return ""
	case GlyphMerged:
		return ""
	case GlyphAlert:
		return ""
	case GlyphMail:
		return ""
	case GlyphPaused:
		return ""
	case GlyphSchedule:
		return ""
	case GlyphBusy:
		return "▍"
	case GlyphQuiet:
		return "│"
	case GlyphChild:
		return "└"
	case GlyphFocus:
		return "›"
	}

	return ""
}
