package view

import (
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
)

// renderRows renders every row of the agents list as its own string.
func renderRows(list List, palette paint.Palette, width int) []string {
	rows := xslice.Map(list.Lines, func(line line) string {
		return renderLine(line, list, palette, width)
	})

	return rows
}

// renderLine spells one prepared row out, painting the selection bar under it last so the
// colours it already carries read through.
func renderLine(line line, list List, palette paint.Palette, width int) string {
	if line.Divider {
		return renderDivider(palette, line.Name, width)
	}

	render := renderer{palette: palette, glyphs: list.Glyphs}
	cells := []string{
		render.gutterCell(line.Focus, plainColumns),
	}
	for i, column := range list.Columns {
		var cell Cell
		if i < len(line.Columns) {
			cell = line.Columns[i]
		}

		cells = append(cells, render.gutterCell(cell, column.Width))
	}

	gutter := strings.Join(cells, " ")

	row := gutter + " " + strings.Repeat(" ", line.Indent) + renderBranch(palette, line.Branch) + renderRule(palette, line.Rule) +
		renderLead(palette, line.Lead) + render.role(line.Role) + renderName(palette, line) + render.trailing(line)

	if len(line.Comments) > 0 {
		comments := strings.Join(xslice.Map(line.Comments, render.cell), " ")
		row += strings.Repeat(" ", max(width-lipgloss.Width(row)-lipgloss.Width(comments), spaceColumns)) + comments
	}

	if line.Selected {
		return palette.PaintRow(row, width)
	}

	return row
}

// renderName is a name in its own lettering, with whatever a search picked out of it rendered in the
// match colour instead.
func renderName(palette paint.Palette, line line) string {
	style := nameStyle(palette, line.Weight, line.Emphasis)
	if !line.Editing {
		return palette.Highlight(line.Name, line.Matches, style)
	}

	r := []rune(line.Name)
	at := max(len(r)-line.Behind, 0)

	return palette.RenderTyped(string(r[:at]), string(r[at:]), style)
}

// renderer spells cells out in a palette and a set of glyphs.
type renderer struct {
	palette paint.Palette
	glyphs  glyph.Set
}

// trailing is the odds and ends after a name, each one a space away from what precedes it.
func (d renderer) trailing(line line) string {
	palette := d.palette
	var b strings.Builder
	for _, cell := range []Cell{line.Working, line.Held, line.Hidden, line.Document, line.Mail, line.Alert} {
		if !cell.IsBlank() {
			b.WriteString(" " + d.cell(cell))
		}
	}

	if !line.Tool.IsBlank() {
		b.WriteString(" " + d.cell(line.Tool))
	}

	if line.Tally > 0 {
		b.WriteString(" " + palette.Style(paint.ToneMuted).Render(label.Tally(line.Tally)))
	}

	if !line.Schedule.IsBlank() {
		b.WriteString(" " + d.cell(line.Schedule))
	}

	if line.Workflow != "" {
		b.WriteString(" " + palette.Style(paint.ToneMuted).Render(line.Workflow))
	}

	if !line.Tail.IsBlank() {
		b.WriteString(" " + d.cell(line.Tail))
	}

	return b.String()
}

// gutterCell is a cell that holds its column whether or not it has anything to render, its text
// set against the right edge of the column.
func (d renderer) gutterCell(cell Cell, width int) string {
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

func renderLead(palette paint.Palette, lead string) string {
	if lead == "" {
		return ""
	}

	return palette.Style(paint.ToneMuted).Render(lead) + " "
}

// role is the mark before a reviewer's name, with the space after it.
func (d renderer) role(cell Cell) string {
	if cell.IsBlank() {
		return ""
	}

	return d.cell(cell) + " "
}

// renderDivider is a rule across the pane titled by name.
func renderDivider(palette paint.Palette, name string, width int) string {
	return palette.TitledRule(palette.Style(paint.ToneMuted).Render(name), "", max(width-1, 1))
}

// renderBranch is the tree a row hangs in, greyed.
func renderBranch(palette paint.Palette, branch string) string {
	if branch == "" {
		return ""
	}

	return palette.Style(paint.ToneMuted).Render(branch)
}

// renderRule is the corner a child hangs off its parent by.
func renderRule(palette paint.Palette, rule bool) string {
	if !rule {
		return ""
	}

	return palette.Style(paint.ToneMuted).Render(glyph.Child) + " "
}

// cell is what a cell holds, in its own tone or each part in its own.
func (d renderer) cell(cell Cell) string {
	if len(cell.Parts) > 0 {
		return strings.Join(xslice.Map(cell.Parts, d.cell), "")
	}

	return d.palette.Style(cell.Tone).Render(d.text(cell))
}

// text is what a cell holds, its text or its glyph spelled out.
func (d renderer) text(cell Cell) string {
	if len(cell.Parts) > 0 {
		return strings.Join(xslice.Map(cell.Parts, d.text), "")
	}
	if cell.Text != "" {
		return cell.Text
	}

	return d.glyphs.Spell(cell.Glyph)
}

// nameStyle is the lettering a name renders in: its weight from where it sits in the tree, its
// foreground from what it has to say.
func nameStyle(palette paint.Palette, weight weight, emphasis emphasis) lipgloss.Style {
	style := lipgloss.NewStyle()

	switch emphasis {
	case emphasisNormal:
	case EmphasisDim:
		style = style.Foreground(palette.Color(paint.ToneMuted))
	case emphasisFocused:
		style = style.Foreground(palette.Color(paint.TonePrimary))
	case emphasisAlert:
		style = style.Bold(true).Foreground(palette.Color(paint.ToneWarning))
	}

	switch weight {
	case weightRegular:
	case weightBold:
		style = style.Bold(true)
	case weightItalic:
		style = style.Italic(true)
	}

	return style
}

// Spell is what a cell holds, its text or its glyph rendered in the set given, without its tone.
func Spell(glyphs glyph.Set, cell Cell) string {
	return renderer{glyphs: glyphs}.text(cell)
}
