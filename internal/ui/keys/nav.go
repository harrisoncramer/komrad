package keys

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/screen"
)

// MoveLabel names the keys that move the cursor a row.
func MoveLabel(k config.Keybindings) string {
	return Label(k.Nav.Up) + "/" + Label(k.Nav.Down)
}

// jumpLabel names the keys that move the cursor to either end.
func jumpLabel(k config.Keybindings) string {
	return doubledLabel(k.Nav.Top) + "/" + Label(k.Nav.Bottom)
}

// NavPageLabel names the keys that move the cursor a page.
func NavPageLabel(k config.Keybindings) string {
	return Label(k.Nav.PageUp) + "/" + Label(k.Nav.PageDown)
}

// ColumnScrollLabel names the keys that scroll a column.
func ColumnScrollLabel(k config.Keybindings) string {
	return Label(k.Column.ScrollDown) + "/" + Label(k.Column.ScrollUp)
}

// MoveRow names the move keys.
func MoveRow[S any]() LegendRow[S] {
	return LegendRow[S]{
		Of:    []ID{Move},
		Desc:  func(S) string { return "move" },
		Label: MoveLabel,
		Strip: true,
	}
}

// NavPageRow names the page keys.
func NavPageRow[S any]() LegendRow[S] {
	return LegendRow[S]{
		Of:    []ID{NavPage},
		Desc:  func(S) string { return "page" },
		Label: NavPageLabel,
	}
}

// JumpRow names the keys to either end.
func JumpRow[S any]() LegendRow[S] {
	return LegendRow[S]{
		Of:    []ID{Jump},
		Desc:  func(S) string { return "top/bottom" },
		Label: jumpLabel,
	}
}

// NavLegend is the four rows the cursor keys are named by.
func NavLegend[S any]() []LegendRow[S] {
	move := MoveRow[S]()
	move.Strip = false

	return []LegendRow[S]{
		move,
		JumpRow[S](),
		NavPageRow[S](),
		{
			Of:    []ID{ColumnScroll},
			Desc:  func(S) string { return "scroll" },
			Label: ColumnScrollLabel,
		},
	}
}

// Navigable is a section the movement keys walk, some rows at a time or to either end.
type Navigable interface {
	// MoveBy walks down the section by rows, up it when rows is negative.
	MoveBy(in screen.Inputs, rows int) screen.Request
	// MoveToEnd lands on the top of the section for a negative direction and the bottom for a
	// positive one.
	MoveToEnd(in screen.Inputs, direction int) screen.Request
}

// NavActionsParams is the section the movement keys walk, the keys bound to them, and the rows a
// page key and a scroll key move it.
type NavActionsParams struct {
	Keys   config.Keybindings
	Nav    Navigable
	In     screen.Inputs
	Page   int
	Scroll int
}

// NavActions is the move, jump, page and scroll keys walking a section.
func NavActions[S any](params NavActionsParams) []Action[S] {
	return []Action[S]{
		{
			ID:   Move,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
			Nav: func(_ S, pressed string) screen.Request {
				return params.Nav.MoveBy(params.In, DirectionOf(pressed, params.Keys.Nav.Up))
			},
		},
		{
			ID:   Jump,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Top, k.Nav.Bottom) },
			Nav: func(_ S, pressed string) screen.Request {
				return params.Nav.MoveToEnd(params.In, DirectionOf(pressed, params.Keys.Nav.Top))
			},
		},
		{
			ID:   NavPage,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
			Nav: func(_ S, pressed string) screen.Request {
				return params.Nav.MoveBy(params.In, DirectionOf(pressed, params.Keys.Nav.PageUp)*params.Page)
			},
		},
		{
			ID:   ColumnScroll,
			Keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
			Nav: func(_ S, pressed string) screen.Request {
				return params.Nav.MoveBy(params.In, DirectionOf(pressed, params.Keys.Column.ScrollUp)*params.Scroll)
			},
		},
	}
}

// NavPageStep is how many rows a page key moves the cursor.
const NavPageStep = 10

// ColumnWheelStep is how many rows one turn of the wheel moves the block beside the list.
const ColumnWheelStep = 3
