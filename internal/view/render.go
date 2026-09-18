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
	lines := make([]string, 0, len(list.Lines))
	for _, line := range list.Lines {
		lines = append(lines, renderLine(line, palette, width))
	}

	return strings.Join(lines, "\n")
}

// renderLine spells one prepared row out, painting the selection bar under it last so the
// colours it already carries read through.
func renderLine(line Line, palette paint.Palette, width int) string {
	gutter := strings.Join([]string{
		drawFocus(palette, line.Focused),
		drawMark(palette, line.Mark),
		drawCell(palette, line.Repo),
		drawCell(palette, line.Review),
		drawCell(palette, line.Forge),
		drawCell(palette, line.Status),
	}, " ")

	row := gutter + " " + strings.Repeat(" ", line.Indent) + drawRule(palette, line.Rule) +
		nameStyle(palette, line.Weight, line.Emphasis).Render(line.Name) + drawTrailing(palette, line)

	if line.Selected {
		return paint.PaintRow(row, width)
	}

	return row
}

// drawTrailing is the odds and ends after a name, each one a space away from what precedes it.
func drawTrailing(palette paint.Palette, line Line) string {
	var b strings.Builder
	for _, cell := range []Cell{line.Held, line.Mail, line.Alert} {
		if cell.Glyph != GlyphNone {
			b.WriteString(" " + drawCell(palette, cell))
		}
	}

	if line.Tally > 0 {
		b.WriteString(" " + palette.Style(paint.ToneMuted).Render(fmt.Sprintf("+%d", line.Tally)))
	}

	if line.Schedule.Glyph != GlyphNone {
		b.WriteString(" " + drawCell(palette, line.Schedule))
	}

	return b.String()
}

// drawFocus is the cell saying which row the keyboard is standing on.
func drawFocus(palette paint.Palette, focused bool) string {
	if !focused {
		return " "
	}

	return palette.Style(paint.ToneAccent).Render(spell(GlyphFocus))
}

// drawMark is the cell holding the letter a person typed onto a row.
func drawMark(palette paint.Palette, mark string) string {
	if mark == "" {
		return " "
	}

	return palette.Style(paint.ToneMark).Render(mark)
}

// drawRule is the corner a child hangs off its parent by.
func drawRule(palette paint.Palette, rule bool) string {
	if !rule {
		return ""
	}

	return palette.Style(paint.ToneMuted).Render(spell(GlyphChild)) + " "
}

// drawCell is one glyph in its own tone.
func drawCell(palette paint.Palette, cell Cell) string {
	return palette.Style(cell.Tone).Render(spell(cell.Glyph))
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
