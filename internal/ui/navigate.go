package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// navigable is a section the movement keys walk, some rows at a time or to either end.
type navigable interface {
	// moveBy walks down the section by rows, up it when rows is negative.
	moveBy(m *model, rows int) tea.Cmd
	// moveToEnd lands on the top of the section for a negative direction and the bottom for a
	// positive one.
	moveToEnd(m *model, direction int) tea.Cmd
}

// navSteps is the rows a page key and a scroll key move a section.
type navSteps struct {
	page   int
	scroll int
}

// navActions is the move, jump, page and scroll keys walking a section.
func navActions(n navigable, steps navSteps) []screenAction {
	return []screenAction{
		{
			id:   actionMove,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
			nav: func(m *model, pressed string) tea.Cmd {
				return n.moveBy(m, directionOf(pressed, m.keys.Nav.Up))
			},
		},
		{
			id:   actionJump,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Top, k.Nav.Bottom) },
			nav: func(m *model, pressed string) tea.Cmd {
				return n.moveToEnd(m, directionOf(pressed, m.keys.Nav.Top))
			},
		},
		{
			id:   actionNavPage,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
			nav: func(m *model, pressed string) tea.Cmd {
				return n.moveBy(m, directionOf(pressed, m.keys.Nav.PageUp)*steps.page)
			},
		},
		{
			id:   actionColumnScroll,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
			nav: func(m *model, pressed string) tea.Cmd {
				return n.moveBy(m, directionOf(pressed, m.keys.Column.ScrollUp)*steps.scroll)
			},
		},
	}
}

// navMoveRow names the move keys.
func navMoveRow() screenLegendRow {
	return screenLegendRow{
		of:    []actionID{actionMove},
		desc:  func(*model) string { return "move" },
		label: moveLabel,
		strip: true,
	}
}

// navPageRow names the page keys.
func navPageRow() screenLegendRow {
	return screenLegendRow{
		of:    []actionID{actionNavPage},
		desc:  func(*model) string { return "page" },
		label: navPageLabel,
	}
}

// navJumpRow names the keys to either end.
func navJumpRow() screenLegendRow {
	return screenLegendRow{
		of:    []actionID{actionJump},
		desc:  func(*model) string { return "top/bottom" },
		label: jumpLabel,
	}
}

// dispatchTop settles the doubled top key ahead of the table.
func (s screenActionSpec) dispatchTop(m *model, k string) tea.Cmd {
	if !m.takeTopPress(k) && m.isTopHeld() {
		return nil
	}

	return s.dispatch(m, k)
}
