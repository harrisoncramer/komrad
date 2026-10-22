package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

// navigable is a section the movement keys walk, some rows at a time or to either end.
type navigable interface {
	// moveBy walks down the section by rows, up it when rows is negative.
	moveBy(in screen.Inputs, rows int) screen.Request
	// moveToEnd lands on the top of the section for a negative direction and the bottom for a
	// positive one.
	moveToEnd(in screen.Inputs, direction int) screen.Request
}

// navSteps is the rows a page key and a scroll key move a section.
type navSteps struct {
	page   int
	scroll int
}

// navActions is the move, jump, page and scroll keys walking a section.
func navActions[S any](bound config.Keybindings, n navigable, in screen.Inputs, steps navSteps) []keys.Action[S] {
	return []keys.Action[S]{
		{
			ID:   keys.Move,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
			Nav: func(_ S, pressed string) screen.Request {
				return n.moveBy(in, keys.DirectionOf(pressed, bound.Nav.Up))
			},
		},
		{
			ID:   keys.Jump,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Top, k.Nav.Bottom) },
			Nav: func(_ S, pressed string) screen.Request {
				return n.moveToEnd(in, keys.DirectionOf(pressed, bound.Nav.Top))
			},
		},
		{
			ID:   keys.NavPage,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
			Nav: func(_ S, pressed string) screen.Request {
				return n.moveBy(in, keys.DirectionOf(pressed, bound.Nav.PageUp)*steps.page)
			},
		},
		{
			ID:   keys.ColumnScroll,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
			Nav: func(_ S, pressed string) screen.Request {
				return n.moveBy(in, keys.DirectionOf(pressed, bound.Column.ScrollUp)*steps.scroll)
			},
		},
	}
}
