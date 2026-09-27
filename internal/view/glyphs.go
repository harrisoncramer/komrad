package view

import (
	"komrad/internal/glyph"
	"komrad/lib/xslice"
)

// Drawn is one glyph the list draws, with the width the layout counts it as.
type Drawn struct {
	Name    string
	Text    string
	Columns int
}

// Drawings is every character the list draws with the set given, and the width its layout
// assumes of each.
func Drawings(set glyph.Set) []Drawn {
	return xslice.Map(set.Drawn(), func(named glyph.Named) Drawn {
		return Drawn{
			Name:    named.Name,
			Text:    named.Text,
			Columns: plainColumns,
		}
	})
}
