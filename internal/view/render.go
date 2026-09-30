package view

import (
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
)

// Rows draws every row of the agents list as its own string.
func Rows(list List, palette paint.Palette, width int) []string {
	rows := xslice.Map(list.Lines, func(line Line) string {
		return renderLine(line, list, palette, width)
	})

	return rows
}

// renderLine spells one prepared row out, painting the selection bar under it last so the
// colours it already carries read through.
func renderLine(line Line, list List, palette paint.Palette, width int) string {
	draw := drawer{palette: palette, glyphs: list.Glyphs}
	cells := []string{
		draw.gutterCell(line.Focus, plainColumns),
	}
	for i, column := range list.Columns {
		var cell Cell
		if i < len(line.Columns) {
			cell = line.Columns[i]
		}

		cells = append(cells, draw.gutterCell(cell, column.Width))
	}

	gutter := strings.Join(append(cells, draw.gutterCell(line.Status, plainColumns)), " ")

	row := gutter + " " + strings.Repeat(" ", line.Indent) + drawRule(palette, line.Rule) +
		drawLead(palette, line.Lead) + drawName(palette, line) + draw.trailing(line)

	if len(line.Comments) > 0 {
		comments := strings.Join(xslice.Map(line.Comments, draw.cell), " ")
		row += strings.Repeat(" ", max(width-lipgloss.Width(row)-lipgloss.Width(comments), spaceColumns)) + comments
	}

	if line.Selected {
		return palette.PaintRow(row, width)
	}

	return row
}

// drawName is a name in its own lettering, with whatever a search picked out of it drawn in the
// match colour instead.
func drawName(palette paint.Palette, line Line) string {
	return palette.Highlight(line.Name, line.Matches, nameStyle(palette, line.Weight, line.Emphasis))
}

// drawer spells cells out in a palette and a set of glyphs.
type drawer struct {
	palette paint.Palette
	glyphs  glyph.Set
}

// trailing is the odds and ends after a name, each one a space away from what precedes it.
func (d drawer) trailing(line Line) string {
	palette := d.palette
	var b strings.Builder
	for _, cell := range []Cell{line.Held, line.Hidden, line.Document, line.Mail, line.Alert} {
		if !cell.IsBlank() {
			b.WriteString(" " + d.cell(cell))
		}
	}

	if line.Tally > 0 {
		b.WriteString(" " + palette.Style(paint.ToneMuted).Render(label.Tally(line.Tally)))
	}

	if !line.Schedule.IsBlank() {
		b.WriteString(" " + d.cell(line.Schedule))
	}

	return b.String()
}

// gutterCell is a cell that holds its column whether or not it has anything to draw, its text
// set against the right edge of the column.
func (d drawer) gutterCell(cell Cell, width int) string {
	if width <= 0 || cell.IsBlank() {
		return strings.Repeat(" ", max(width, 0))
	}

	text := d.text(cell)
	if lipgloss.Width(text) > width || len(cell.Parts) == 0 {
		text = paint.Clip(text, width)

		return strings.Repeat(" ", max(width-lipgloss.Width(text), 0)) + d.palette.Style(cell.Tone).Render(text)
	}

	return strings.Repeat(" ", width-lipgloss.Width(text)) + d.cell(cell)
}

func drawLead(palette paint.Palette, lead string) string {
	if lead == "" {
		return ""
	}

	return palette.Style(paint.ToneMuted).Render(lead) + " "
}

// drawRule is the corner a child hangs off its parent by.
func drawRule(palette paint.Palette, rule bool) string {
	if !rule {
		return ""
	}

	return palette.Style(paint.ToneMuted).Render(glyph.Child) + " "
}

// cell is what a cell holds, in its own tone or each part in its own.
func (d drawer) cell(cell Cell) string {
	if len(cell.Parts) > 0 {
		return strings.Join(xslice.Map(cell.Parts, d.cell), "")
	}

	return d.palette.Style(cell.Tone).Render(d.text(cell))
}

// text is what a cell holds, its text or its glyph spelled out.
func (d drawer) text(cell Cell) string {
	if len(cell.Parts) > 0 {
		return strings.Join(xslice.Map(cell.Parts, d.text), "")
	}
	if cell.Text != "" {
		return cell.Text
	}

	return d.glyphs.Spell(cell.Glyph)
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
		style = style.Foreground(palette.Color(paint.TonePrimary))
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

// Spell is what a cell holds, its text or its glyph drawn in the set given, without its tone.
func Spell(glyphs glyph.Set, cell Cell) string {
	return drawer{glyphs: glyphs}.text(cell)
}
