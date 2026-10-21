package ui

import (
	"strconv"
	"strings"

	"komrad/internal/ui/paint"
	"komrad/internal/ui/reader"
)

// docHead is the rows over the document while it holds the keys or a search: the search and what
// the last key said.
func (m *model) docHead(rows []string, width int) []string {
	r := m.reading
	if !m.isDocFocused() && !r.filter.typing && !r.filter.isStanding() {
		return nil
	}

	count := ""
	if r.filter.isSearched() {
		count = dimStyle().Render("  " + reader.Count(reader.Matches(r.filter.pattern, rows), r.Line))
	}

	return r.filter.head(m, count, r.said, width)
}

// docBlock is the document at the width of the block beside the list, under the search over it,
// from where the reader has moved to.
func (m *model) docBlock(split columnSplit) []string {
	rows := m.columnRows(split)
	head := m.docHead(rows, split.column)
	height := max(split.rows-len(head), 1)

	r := m.reading
	focused := m.isDocFocused()
	rendered := make([]string, len(rows))
	for i, row := range rows {
		rendered[i] = paint.Panel.HighlightStyled(row, reader.Spans(r.filter.pattern, row))
		if focused && i == r.Line {
			rendered[i] = paint.Panel.PaintRow(rendered[i], split.column)
		}
		rendered[i] = m.markRow(zoneDoc, split.column, strconv.Itoa(i), rendered[i])
	}

	window := docWindow{
		rows:    rendered,
		top:     r.Top,
		markers: true,
	}
	if r.tail {
		window.top = len(rendered)
	}

	return append(head, strings.Split(window.view(height), "\n")...)
}
