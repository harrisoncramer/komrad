package ui

import (
	"slices"
	"strings"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/list"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
	"komrad/internal/ui/zone"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

func (m *model) activeListParams(in screen.Inputs) (list.Params, bool) {
	l, ok := m.active().(listRenderer)
	if !ok {
		return list.Params{}, false
	}

	return l.ListParams(in), true
}

// listHelp is a list screen's legend: its own rows, then the bindings every list shares.
func (m *model) listHelp(own legend.Help) legend.Help {
	// A disabled binding renders nothing but still takes a cell of the expanded legend.
	var tabs []key.Binding
	if b := m.tabsBinding(); b.Enabled() {
		tabs = []key.Binding{b}
	}
	if b := m.kindsBinding(); b.Enabled() {
		tabs = append(tabs, b)
	}

	return legend.Help{
		Short: slices.Concat(own.Short, m.globalHelp()),
		Full: slices.Concat(
			own.Full,
			tabs,
			m.fullGlobalHelp(),
		),
	}
}

// Lines is the room the list has once the chrome around it has taken its share.
func (m *model) Lines(chrome ...string) int {
	return fitLines(m.term.height, chrome...)
}

// fitLines is the room a pane of height lines has once the chrome has taken its share.
func fitLines(height int, chrome ...string) int {
	if height <= 0 {
		return paint.Unbounded
	}

	return max(spareLinesOf(height, chrome...), 0)
}

// spareLines is what the pane has left once the chrome has taken its share, unclamped.
func (m *model) spareLines(chrome ...string) int {
	return spareLinesOf(m.term.height, chrome...)
}

// spareLinesOf is what a pane of height lines has left once the chrome has taken its share.
func spareLinesOf(height int, chrome ...string) int {
	if height <= 0 {
		return 0
	}

	lines := height
	for _, part := range chrome {
		lines -= strings.Count(part, "\n")
	}

	return lines
}

func (m *model) listClicker(l keys.KeyedList) clicker {
	clicked := requestClick(func(m *model, key string, nth int) screen.Request { return l.Click(m.inputs(), key, nth) })
	if !m.isListFocused(m.inputs()) || m.isViewsOpen() {
		return m.stopClicker(stopList, clicked)
	}

	return clickFunc(func(m *model, key string, nth int) tea.Cmd {
		if key != l.Hovered(m.inputs()) {
			return clicked.click(m, key, nth)
		}

		band, ok := m.buildMenu()
		if !ok {
			return nil
		}

		return m.apply(menu.Open{Menu: band})
	})
}

func (m *model) isHoveredHit(hit zone.Hit) bool {
	lister, ok := m.active().(activeLister)

	return ok && hit.Zone == zone.List && hit.Key == lister.ActiveList().Hovered(m.inputs())
}
