package ui

import (
	"strconv"
	"strings"

	"komrad/internal/ui/column"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/reader"
	"komrad/internal/ui/screen"
)

// docHead is the rows over the document while it holds the keys or a search: the search and what
// the last key said.
func (m *model) docHead(in screen.Inputs, rows []string, width int) []string {
	r := m.reading
	if !in.Room.DocumentReader.HoldsKeys && !r.filter.IsTyping && !r.filter.IsStanding() {
		return nil
	}

	count := ""
	if r.filter.IsSearched() {
		count = paint.Panel.Style(paint.ToneMuted).Render("  " + reader.Count(reader.Matches(r.filter.Matcher, rows), r.CursorLine))
	}

	return r.filter.Head(filter.HeadParams{Glyphs: m.glyphs, ShouldMarkZones: in.Room.ShouldMarkZones, MatchCount: count, SaidText: r.said, Width: width})
}

// docBlock is the document at the width of the block beside the list, under the search over it,
// from where the reader has moved to.
func (m *model) docBlock(in screen.Inputs, split column.Split) []string {
	rows := m.columnRows(in, split)
	head := m.docHead(in, rows, split.ColumnWidth)
	height := max(split.RowCount-len(head), 1)

	r := m.reading
	focused := in.Room.DocumentReader.HoldsKeys
	rendered := make([]string, len(rows))
	for i, row := range rows {
		rendered[i] = paint.Panel.HighlightStyled(row, reader.Spans(r.filter.Matcher, row))
		if focused && i == r.CursorLine {
			rendered[i] = paint.Panel.PaintRow(rendered[i], split.ColumnWidth)
		}
		rendered[i] = m.markRow(zoneDoc, split.ColumnWidth, strconv.Itoa(i), rendered[i])
	}

	window := reader.Page{
		Rows:       rendered,
		TopLine:    r.TopLine,
		HasMarkers: true,
	}
	if r.tail {
		window.TopLine = len(rendered)
	}

	return append(head, strings.Split(window.View(height), "\n")...)
}
