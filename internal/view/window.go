package view

import "komrade/internal/ui/paint"

// WindowParams is a prepared list and the pane it is being fitted to.
type WindowParams struct {
	List    List
	Palette paint.Palette
	// Width is the pane a selected row's bar is filled out to.
	Width int
	// Lines is how many rows of the pane the list has to itself.
	Lines int
}

// Window is the screenful of a list a pane holds, scrolled so the focused row is inside it and
// carrying the counts of the rows that fall either side.
func Window(params WindowParams) string {
	total := len(params.List.Lines)
	window := paint.WindowAround(total, focusIndex(params.List), params.Lines)
	shown := List{
		Lines:   params.List.Lines[window.Start:window.End],
		Columns: params.List.Columns,
	}

	return paint.DrawWindow(Rows(shown, params.Palette, params.Width), window, total, params.Palette.Style(paint.ToneMuted))
}

// focusIndex is the row the keyboard stands on, counted off the list as it is about to be
// drawn.
func focusIndex(list List) int {
	for i, line := range list.Lines {
		if line.Focused {
			return i
		}
	}

	// A key naming no row leaves the list where a fresh one opens, at the top.
	return 0
}
