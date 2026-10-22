package keys

import "komrad/config"

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
