package view

import (
	"strings"

	"weave/internal/ui/paint"
	"weave/internal/ui/paint/label"
	"weave/lib/xslice"

	"github.com/charmbracelet/lipgloss"
)

// Rows draws every row of the agents list as its own string.
func Rows(list List, palette paint.Palette, width int) []string {
	rows := xslice.Map(list.Lines, func(line Line) string {
		return renderLine(line, list.Columns, palette, width)
	})

	return rows
}

// renderLine spells one prepared row out, painting the selection bar under it last so the
// colours it already carries read through.
func renderLine(line Line, columns Columns, palette paint.Palette, width int) string {
	cells := []string{
		drawGutterCell(palette, line.Focus),
		drawGutterCell(palette, line.Mark),
	}
	cells = append(cells, xslice.Map(drawnColumnCells(line, columns), func(cell Cell) string {
		return drawGutterCell(palette, cell)
	})...)

	gutter := strings.Join(append(cells, drawGutterCell(palette, line.Status)), " ")

	row := gutter + " " + strings.Repeat(" ", line.Indent) + drawRule(palette, line.Rule) +
		drawName(palette, line) + drawTrailing(palette, line)
	row += drawTrail(palette, line, width-lipgloss.Width(row))

	if line.Selected {
		return paint.PaintRow(row, width)
	}

	return row
}

// drawName is a name in its own lettering, with whatever a search picked out of it drawn in the
// match colour instead.
func drawName(palette paint.Palette, line Line) string {
	return palette.Highlight(line.Name, line.Matches, nameStyle(palette, line.Weight, line.Emphasis))
}

// drawTrailing is the odds and ends after a name, each one a space away from what precedes it.
func drawTrailing(palette paint.Palette, line Line) string {
	var b strings.Builder
	for _, cell := range []Cell{line.Held, line.Hidden, line.Mail, line.Alert} {
		if !cell.IsBlank() {
			b.WriteString(" " + drawCell(palette, cell))
		}
	}

	if line.Tally > 0 {
		b.WriteString(" " + palette.Style(paint.ToneMuted).Render(label.Tally(line.Tally)))
	}

	if !line.Schedule.IsBlank() {
		b.WriteString(" " + drawCell(palette, line.Schedule))
	}

	return b.String()
}

// drawTrail is the text at the right edge, padded across the columns left after what precedes
// it.
func drawTrail(palette paint.Palette, line Line, left int) string {
	if line.Trail == "" {
		return ""
	}

	pad := max(left-lipgloss.Width(line.Trail), spaceColumns)

	return strings.Repeat(" ", pad) + palette.Style(paint.ToneMuted).Render(line.Trail)
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

	return palette.Style(paint.ToneMuted).Render(Spell(GlyphChild)) + " "
}

// drawCell is what a cell holds, in its own tone.
func drawCell(palette paint.Palette, cell Cell) string {
	text := cell.Text
	if text == "" {
		text = Spell(cell.Glyph)
	}

	return palette.Style(cell.Tone).Render(text)
}

// nameStyle is the lettering a name is drawn in: its weight from where it sits in the tree, its
// foreground from what it has to say.
func nameStyle(palette paint.Palette, weight Weight, emphasis Emphasis) lipgloss.Style {
	style := lipgloss.NewStyle()

	switch emphasis {
	case EmphasisNormal:
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

// Spell is the one place a glyph is written out, every one of them a Nerd Font Octicon or a
// box-drawing rule.
func Spell(glyph Glyph) string {
	switch glyph {
	case GlyphNone, glyphCount:
		return ""
	case GlyphRepo:
		return ""
	case GlyphUnverified:
		return "\uf4a3"
	case GlyphForge:
		return ""
	case GlyphApproved:
		return ""
	case GlyphBroken:
		return ""
	case GlyphCheckbox:
		return "\uf4a7"
	case GlyphBell:
		return "\uf49a"
	case GlyphHourglass:
		return "\uf4e3"
	case GlyphMerged:
		return ""
	case GlyphDraft:
		return "\uf4dd"
	case GlyphClosed:
		return "\uf4dc"
	case GlyphAlert:
		return ""
	case GlyphMail:
		return ""
	case GlyphPaused:
		return ""
	case GlyphHidden:
		return ""
	case GlyphSchedule:
		return ""
	case GlyphWorkflow:
		return "\uf52e"
	case GlyphArchive:
		return "\uf411"
	case GlyphSearch:
		return "\uf422"
	case GlyphBusy:
		return "┃"
	case GlyphQuiet:
		return "│"
	case GlyphChild:
		return "└"
	case GlyphFocus:
		return "›"
	}

	return ""
}
