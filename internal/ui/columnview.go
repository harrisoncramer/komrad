package ui

import (
	"strings"

	"komrad/internal/glyph"
	"komrad/internal/ui/column"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// joinColumns lays the sidebar left of the column, having squared each off at its share first.
func (m *model) joinColumns(split column.Split, sidebar, block string) string {
	// Trimmed so the two halves are the same height.
	sidebar = fitColumn(strings.TrimRight(sidebar, "\n"), split.Sidebar)
	block = m.markZone(zoneColumn, fitColumn(block, split.Column), split.Column)

	sidebarLines, columnLines := strings.Split(sidebar, "\n"), strings.Split(block, "\n")
	rows := max(len(sidebarLines), len(columnLines))
	sidebarWidth, columnWidth := lipgloss.Width(sidebar), lipgloss.Width(block)

	out := make([]string, rows)
	for i := range rows {
		sidebarLine := readLineAt(sidebarLines, i, sidebarWidth)
		columnLine := readLineAt(columnLines, i, columnWidth)
		out[i] = sidebarLine + renderDividerRow(isRule(sidebarLine), isRule(columnLine)) + columnLine
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
	split := m.inputs().Room.Split
	if !split.On {
		return list(m)
	}

	// Asked for before either half is rendered, so the halves are laid out already knowing their
	// column.
	held := m.measuredAt(split.Sidebar)

	return m.joinColumns(split, list(held), held.docColumnView(split))
}

// docColumnView is the first screenful of the block beside the list, with a marker under it
// counting what is past it.
func (m *model) docColumnView(split column.Split) string {
	return m.columnDoc(m.inputs(), split).view(split.Rows)
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
