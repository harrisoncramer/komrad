package ui

import (
	"slices"
	"strings"

	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/record"
	"komrad/internal/view"
	"komrad/lib/xslice"

	"charm.land/lipgloss/v2"
)

// flashFieldStyle marks a row or a field the user has just copied.
func flashFieldStyle(palette paint.Palette) lipgloss.Style {
	return lipgloss.NewStyle().Background(palette.Color(paint.TonePrimary)).Foreground(palette.Color(paint.ToneInverse))
}

func (m *model) detailsView() string {
	in := m.inputs()
	held := m.hoveredDetails(in)
	if !m.isDetailsRendered(held) {
		return ""
	}

	lines := m.detailsLines()
	if m.details.focused {
		lines = m.grownDetailsLines(in, held)
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

	pane := m.paneWidth()
	body := dimStyle().Render("  nothing matches")
	if fields := m.detailsRows(held); len(fields) > 0 || !m.filter.pattern().DoesNarrow(list.SectionRecord) {
		snapshot := m.detailsSnapshot(fields, m.detailsCursor(held), pane)
		body = view.Window(view.WindowParams{
			List:    list.Prepare(m.glyphs, snapshot),
			Palette: paint.Panel,
			Width:   pane,
			Lines:   lines,
			Mark:    m.rowMarker(zoneDetails, snapshot.Rows, pane),
		})
	}

	return m.markZone(zoneDetails, m.detailsRule("Info", held.Tags...)+
		block+
		paint.PadRows(body, lines)+
		m.detailsRule(""), pane)
}

// Record is the hovered row's record, or the rule standing in for it while the pane has the room.
func (m *model) Record(above ...string) string {
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
func (m *model) detailsNotices(held record.Details, budget int) []string {
	if budget <= 0 {
		return nil
	}

	rendered := []string{}
	for _, n := range held.Notices {
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

// noticeLines renders one notice, putting the glyph the row carries first, wrapping the sentence
// under it in the column the glyph left, and cutting off at a number of lines when given one.
func (m *model) noticeLines(n record.Notice, most int) []string {
	const indent = "    "

	// A notice that is going to be cut keeps a cell back for the ellipsis.
	width := m.lineWidth() - len(indent)
	if most > 0 {
		width--
	}

	text := paint.WrapRows(n.Text, width)
	if most > 0 && len(text) > most {
		text = text[:most]
		text[most-1] += "…"
	}

	icon := " "
	if n.Icon != "" {
		icon = paint.Panel.Style(n.Tone).Render(n.Icon)
	}

	lines := []string{"  " + icon + " " + dimStyle().Render(text[0])}
	lines = append(lines, xslice.Map(text[1:], func(line string) string {
		return indent + dimStyle().Render(line)
	})...)

	return lines
}

func (m *model) detailsSnapshot(fields []detailsRow, cursor, pane int) view.Snapshot {
	labels := labelColumn(xslice.Map(fields, func(row detailsRow) record.Field { return row.field }))
	flashed := list.KeyAt(fields, m.details.flashed-1)

	return view.Snapshot{
		Rows: xslice.Map(fields, func(row detailsRow) view.Row {
			return view.Row{
				Key:      row.id,
				Kind:     view.KindResult,
				Lead:     padRight(row.field.Label, labels),
				Name:     row.field.Value,
				Matches:  matchesIn(m.filter.matcher, row.field.Value),
				Process:  view.ProcessNone,
				Tail:     row.field.Tail,
				Selected: m.details.flashed > 0 && row.id == flashed,
			}
		}),
		Caret:   m.caretOf(m.inputs(), list.SectionRecord),
		Focused: list.KeyAt(fields, cursor),
		Pane:    pane,
		Columns: view.Columns{},
	}
}

func (m *model) lineWidth() int {
	return lineWidthOf(m.paneWidth())
}

// lineWidthOf is the column a line of prose wraps at, in a pane of the width given.
func lineWidthOf(pane int) int {
	if pane <= 0 {
		return nameWidth
	}

	return pane
}

func labelColumn(fields []record.Field) int {
	widest := 0
	for _, field := range fields {
		widest = max(widest, len([]rune(field.Label)))
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
