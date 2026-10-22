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
	moveBy(m *model, rows int) screen.Request
	// moveToEnd lands on the top of the section for a negative direction and the bottom for a
	// positive one.
	moveToEnd(m *model, direction int) screen.Request
}

// navSteps is the rows a page key and a scroll key move a section.
type navSteps struct {
	page   int
	scroll int
}

// navActions is the move, jump, page and scroll keys walking a section.
func navActions(n navigable, steps navSteps) []keys.Action[*model] {
	return []keys.Action[*model]{
		{
			ID:   keys.Move,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
			Nav: func(m *model, pressed string) screen.Request {
				return n.moveBy(m, keys.DirectionOf(pressed, m.keys.Nav.Up))
			},
		},
		{
			ID:   keys.Jump,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Top, k.Nav.Bottom) },
			Nav: func(m *model, pressed string) screen.Request {
				return n.moveToEnd(m, keys.DirectionOf(pressed, m.keys.Nav.Top))
			},
		},
		{
			ID:   keys.NavPage,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
			Nav: func(m *model, pressed string) screen.Request {
				return n.moveBy(m, keys.DirectionOf(pressed, m.keys.Nav.PageUp)*steps.page)
			},
		},
		{
			ID:   keys.ColumnScroll,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
			Nav: func(m *model, pressed string) screen.Request {
				return n.moveBy(m, keys.DirectionOf(pressed, m.keys.Column.ScrollUp)*steps.scroll)
			},
		},
	}
}

// dispatchTop settles the doubled top key ahead of the table.
func (m *model) dispatchTop(spec keys.Spec[*model], pressed string) screen.Request {
	if !m.takeTopPress(pressed) && m.isTopHeld() {
		return nil
	}

	return spec.Dispatch(m, m.keys, pressed)
}

// isTopHeld reports whether the last press opened the doubled top key and is waiting on its pair.
func (m *model) isTopHeld() bool {
	return m.topHeld
}

func (m *model) takeTopPress(pressed string) bool {
	held := m.topHeld
	m.topHeld = !held && keys.IsBound(pressed, m.keys.Nav.Top)

	return held && keys.IsBound(pressed, m.keys.Nav.Top)
}

func (m *model) dropTopPress() {
	m.topHeld = false
}
