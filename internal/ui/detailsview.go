package ui

import (
	"slices"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/view"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// flashFieldStyle marks a row or a field that has just been copied.
func flashFieldStyle(palette paint.Palette) lipgloss.Style {
	return lipgloss.NewStyle().Background(palette.Color(paint.TonePrimary)).Foreground(palette.Color(paint.ToneInverse))
}

func (m *model) detailsView() string {
	held := m.hoveredDetails()
	if !m.isDetailsRendered(held) {
		return ""
	}

	lines := m.detailsLines()
	if m.details.focused {
		lines = m.grownDetailsLines(held)
	}
	if lines == 0 {
		return ""
	}

	// Two field lines are kept back rather than one.
	notices := m.detailsNotices(held, lines-3)
	block := ""
	if len(notices) > 0 {
		block = strings.Join(notices, "\n") + "\n\n"
		lines -= len(notices) + 1
	}

	cursor := m.detailsCursor(held)
	rows := m.detailFieldRows(m.detailsRows(held), cursor)
	if len(rows) == 0 && m.filter.doesNarrow(sectionRecord) {
		rows = []string{dimStyle().Render("  nothing matches")}
	}

	return m.markZone(zoneDetails, m.detailsRule("Info", held.tags...)+
		block+
		paint.PadRows(paint.RenderList(rows, cursor, lines, dimStyle()), lines)+
		m.detailsRule(""), m.paneWidth())
}

func (m *model) renderRecord(above ...string) string {
	if record := m.detailsView(); record != "" {
		return record
	}

	rule := m.detailsRule("")
	if m.term.height > 0 && m.spareLines(append(slices.Clone(above), rule)...) < legendRuleFloor {
		return ""
	}

	return rule
}

// detailsRule is a rule of the record titled by title, carrying as many of its tags as fit with
// the leftmost given up first.
func (m *model) detailsRule(title string, tags ...string) string {
	width := m.paneWidth()
	if width <= 0 {
		return "\n"
	}

	tags = slices.DeleteFunc(slices.Clone(tags), func(tag string) bool { return tag == "" })
	label := dimStyle().Render(title)
	tail := strings.Join(tags, dimStyle().Render(separated))
	for len(tags) > 0 && !paint.TitledRuleFits(label, tail, width) {
		tags = tags[1:]
		tail = strings.Join(tags, dimStyle().Render(separated))
	}

	return paint.Panel.TitledRule(label, tail, width) + "\n"
}

// detailsNotices are the lines the marks on the row take above the fields, giving up a notice
// that does not fit rather than the fields under it.
func (m *model) detailsNotices(held details, budget int) []string {
	if budget <= 0 {
		return nil
	}

	rendered := []string{}
	for _, n := range held.notices {
		lines := m.noticeLines(n, 0)
		if len(rendered)+len(lines) > budget {
			// The first notice is cut short rather than given up.
			if len(rendered) > 0 {
				break
			}

			lines = m.noticeLines(n, budget)
		}

		rendered = append(rendered, lines...)
	}

	return rendered
}

// noticeLines is one notice as it is rendered: the glyph the row carries, then the sentence wrapped
// under it in the column the glyph left, cut off at a number of lines when given one.
func (m *model) noticeLines(n notice, most int) []string {
	const indent = "    "

	// A notice that is going to be cut keeps a cell back for the ellipsis.
	width := m.lineWidth() - len(indent)
	if most > 0 {
		width--
	}

	text := paint.WrapRows(n.text, width)
	if most > 0 && len(text) > most {
		text = text[:most]
		text[most-1] += "…"
	}

	icon := " "
	if n.icon != "" {
		icon = paint.Panel.Style(n.tone).Render(n.icon)
	}

	lines := []string{"  " + icon + " " + dimStyle().Render(text[0])}
	lines = append(lines, xslice.Map(text[1:], func(line string) string {
		return indent + dimStyle().Render(line)
	})...)

	return lines
}

func (m *model) detailFieldRows(fields []detailsRow, cursor int) []string {
	labels := labelColumn(xslice.Map(fields, func(row detailsRow) recordField { return row.field }))
	rows := make([]string, 0, len(fields))

	const gutter = 2

	for i, row := range fields {
		prefix, labelStyle := "  ", dimStyle()
		if i == cursor {
			prefix = m.cursorPrefix(sectionRecord)
			if m.caretOf(sectionRecord) != view.CaretAbsent {
				labelStyle = rowCursorStyle()
			}
		}

		label := m.filter.highlight(paint.Panel, padRight(row.field.label, labels), labelStyle)
		width := max(m.lineWidth()-labels-gutter-1, 1)
		value := paint.Clip(row.field.value, width)
		switch {
		case m.details.flashed == i+1:
			value = flashFieldStyle(paint.Panel).Render(value)
		case m.filter.pattern != nil:
			value = m.filter.highlight(paint.Panel, value, lipgloss.NewStyle())
		case row.field.styled != "":
			value = ansi.Truncate(row.field.styled, width, "…")
		}

		rows = append(rows, prefix+label+" "+value)
	}

	return rows
}

func (m *model) lineWidth() int {
	return lineWidthOf(m.paneWidth())
}

// lineWidthOf is the column a line of prose is wrapped into, in a pane of the width given.
func lineWidthOf(pane int) int {
	if pane <= 0 {
		return nameWidth
	}

	return pane
}

func labelColumn(fields []recordField) int {
	widest := 0
	for _, field := range fields {
		widest = max(widest, len([]rune(field.label)))
	}

	return widest
}

func padRight(text string, width int) string {
	pad := width - len([]rune(text))
	if pad <= 0 {
		return text
	}

	return text + strings.Repeat(" ", pad)
}
