package ui

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/zone"
)

type filterHeadParams struct {
	glyphs glyph.Set
	marks  bool
	count  string
	said   string
	width  int
}

// filterHead is the rows over a block the filter searches: the search, what the last key said, and a
// blank row.
func filterHead(f filter.State, params filterHeadParams) []string {
	search := paint.FitRow(f.Line(params.glyphs)+params.count, params.width)
	if params.marks {
		search = zone.MarkKey(zone.Search, params.width, 0, zone.SearchBlock) + search
	}

	head := []string{search}
	if params.said != "" {
		head = append(head, paint.FitRow(paint.Panel.Style(paint.ToneMuted).Render(params.said), params.width))
	}

	return append(head, "")
}
