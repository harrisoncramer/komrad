package reader

// Page is where in a document the reader is: the rows it was rendered to, the first of them on
// screen, and whether it counts off what is past either end.
type Page struct {
	Rows    []string
	TopLine int
	// HasMarkers is whether the page counts the rows above and below it, which a document read from
	// the top wants and a transcript opened on a match does not.
	HasMarkers bool
}

// Shape is the page as a window measures it.
func (p Page) Shape() Window {
	return Window{
		DocumentLength: len(p.Rows),
		TopLine:        p.TopLine,
		HasMarkers:     p.HasMarkers,
	}
}
