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
	if !in.Room.Doc.Focused && !r.filter.Typing && !r.filter.IsStanding() {
		return nil
	}

	count := ""
	if r.filter.IsSearched() {
		count = paint.Panel.Style(paint.ToneMuted).Render("  " + reader.Count(reader.Matches(r.filter.Matcher, rows), r.Line))
	}

	return r.filter.Head(filter.HeadParams{Glyphs: m.glyphs, Marks: in.Room.Marks, Count: count, Said: r.said, Width: width})
}

// docBlock is the document at the width of the block beside the list, under the search over it,
// from where the reader has moved to.
func (m *model) docBlock(in screen.Inputs, split column.Split) []string {
	rows := m.columnRows(in, split)
	head := m.docHead(in, rows, split.Column)
	height := max(split.Rows-len(head), 1)

	r := m.reading
	focused := in.Room.Doc.Focused
	rendered := make([]string, len(rows))
	for i, row := range rows {
		rendered[i] = paint.Panel.HighlightStyled(row, reader.Spans(r.filter.Matcher, row))
		if focused && i == r.Line {
			rendered[i] = paint.Panel.PaintRow(rendered[i], split.Column)
		}
		rendered[i] = m.markRow(zoneDoc, split.Column, strconv.Itoa(i), rendered[i])
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
