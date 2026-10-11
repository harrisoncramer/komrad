package ui

import (
	"strings"

	"komrad/internal/glyph"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// joinColumns lays the two halves of the window side by side, on the side the frame puts the
// sidebar, having squared each off at its share first.
func (m *model) joinColumns(split columnSplit, sidebar, column string) string {
	// Trimmed so the two halves are the same height.
	sidebar = fitColumn(strings.TrimRight(sidebar, "\n"), split.sidebar)
	column = m.markZone(zoneColumn, fitColumn(column, split.column), split.column)

	left, right := sidebar, column
	if m.frame.IsSidebarOnRight() {
		left, right = column, sidebar
	}

	leftLines, rightLines := strings.Split(left, "\n"), strings.Split(right, "\n")
	rows := max(len(leftLines), len(rightLines))
	leftWidth, rightWidth := lipgloss.Width(left), lipgloss.Width(right)

	out := make([]string, rows)
	for i := range rows {
		l := readLineAt(leftLines, i, leftWidth)
		r := readLineAt(rightLines, i, rightWidth)
		out[i] = l + renderDividerRow(isRule(l), isRule(r)) + r
	}

	return strings.Join(out, "\n")
}

// readLineAt is the row of a half at i, or blank cells when the half is shorter than the other.
func readLineAt(lines []string, i, width int) string {
	if i < len(lines) {
		return lines[i]
	}

	return strings.Repeat(" ", width)
}

// isRule reports whether a row is a rule across the whole of its half.
func isRule(line string) bool {
	plain := ansi.Strip(line)

	return plain != "" && strings.Trim(plain, glyph.Rule) == ""
}

// renderDividerRow is one row of the divider, joined to the rules that meet it from either side.
func renderDividerRow(fromLeft, fromRight bool) string {
	junction := dividerGlyph
	switch {
	case fromLeft && fromRight:
		junction = glyph.Cross
	case fromLeft:
		junction = glyph.TeeRight
	case fromRight:
		junction = glyph.TeeLeft
	}

	leftPad, rightPad := strings.Repeat(" ", gutter), strings.Repeat(" ", gutter)
	if fromLeft {
		leftPad = dimStyle().Render(strings.Repeat(glyph.Rule, gutter))
	}
	if fromRight {
		rightPad = dimStyle().Render(strings.Repeat(glyph.Rule, gutter))
	}

	return leftPad + dimStyle().Render(junction) + rightPad
}

// columnView renders a screen as its list beside its own block, or as the list alone on a window
// that will not hold both.
func (m *model) columnView(list func(*model) string) string {
	split, ok := m.screenColumnSplit()
	if !ok {
		return list(m)
	}

	// Asked for before either half is rendered, so the halves are laid out already knowing their
	// column.
	held := m.measuredAt(split.sidebar)

	return m.joinColumns(split, list(held), held.docColumnView(split))
}

// docColumnView is the first screenful of the block beside the list, with a marker under it
// counting what is past it.
func (m *model) docColumnView(split columnSplit) string {
	return m.columnDoc(split).view(split.rows)
}

// fitColumn squares a block off at a width.
func fitColumn(block string, width int) string {
	rows := strings.Split(block, "\n")
	for i, row := range rows {
		if lipgloss.Width(row) > width {
			row = ansi.Truncate(row, width, "")
		}

		rows[i] = row + strings.Repeat(" ", max(width-lipgloss.Width(row), 0))
	}

	return strings.Join(rows, "\n")
}
