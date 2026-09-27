package view

import (
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
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
		drawGutterCell(palette, line.Focus, plainColumns),
		drawGutterCell(palette, line.Mark, plainColumns),
	}
	for i, column := range columns {
		var cell Cell
		if i < len(line.Columns) {
			cell = line.Columns[i]
		}

		cells = append(cells, drawGutterCell(palette, cell, column.Width))
	}

	gutter := strings.Join(append(cells, drawGutterCell(palette, line.Status, plainColumns)), " ")

	row := gutter + " " + strings.Repeat(" ", line.Indent) + drawRule(palette, line.Rule) +
		drawLead(palette, line.Lead) + drawName(palette, line) + drawTrailing(palette, line)

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
	for _, cell := range []Cell{line.Held, line.Hidden, line.Document, line.Mail, line.Alert} {
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

// drawGutterCell is a cell that holds its column whether or not it has anything to draw, its
// text set against the right edge of the column.
func drawGutterCell(palette paint.Palette, cell Cell, width int) string {
	if width <= 0 || cell.IsBlank() {
		return strings.Repeat(" ", max(width, 0))
	}

	text := cellText(cell)
	if lipgloss.Width(text) > width || len(cell.Parts) == 0 {
		text = paint.Clip(text, width)

		return strings.Repeat(" ", max(width-lipgloss.Width(text), 0)) + palette.Style(cell.Tone).Render(text)
	}

	return strings.Repeat(" ", width-lipgloss.Width(text)) + drawCell(palette, cell)
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

	return palette.Style(paint.ToneMuted).Render(Spell(GlyphChild)) + " "
}

// drawCell is what a cell holds, in its own tone or each part in its own.
func drawCell(palette paint.Palette, cell Cell) string {
	if len(cell.Parts) > 0 {
		return strings.Join(xslice.Map(cell.Parts, func(part Cell) string {
			return drawCell(palette, part)
		}), "")
	}

	return palette.Style(cell.Tone).Render(cellText(cell))
}

// cellText is what a cell holds, the letter a person typed or the glyph spelled out.
func cellText(cell Cell) string {
	if len(cell.Parts) > 0 {
		return strings.Join(xslice.Map(cell.Parts, cellText), "")
	}
	if cell.Text != "" {
		return cell.Text
	}

	return Spell(cell.Glyph)
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
	case GlyphForge:
		return ""
	case GlyphApproved:
		return ""
	case GlyphBroken:
		return ""
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
	case GlyphFailing:
		return "\uf467"
	case GlyphConflicting:
		return "\uf421"
	case GlyphDocument:
		return "\uf4a5"
	}

	return ""
}
