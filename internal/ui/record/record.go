package record

import (
	"komrad/internal/ui/paint"
	"komrad/internal/view"
)

type Details struct {
	ID      string
	Notices []Notice
	Fields  []Field
	Tags    []string
}

type Field struct {
	Label string
	Value string
	Tail  view.Cell
}

// Notice is one of the marks a row renders said in words, carrying the part it plays rather than a
// colour.
type Notice struct {
	Icon string
	Tone paint.Tone
	Text string
}
