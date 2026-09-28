package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the logs screen as one table and the legend that names it.
func (l *logsScreen) actions() screenActionSpec {
	return screenActionSpec{
		table:  l.table(),
		legend: l.legend(),
	}
}

func (l *logsScreen) table() []screenAction {
	return []screenAction{
		{
			id:   actionJump,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Top, k.Nav.Bottom) },
			run: func(_ host, s panelShared, pressed string) tea.Cmd {
				if isBound(pressed, s.keys.Nav.Bottom) {
					l.viewer.Bottom()
				}

				return nil
			},
		},
		{
			id:   actionQuit,
			keys: func(k config.Keybindings) []string { return k.Logs.Quit },
			run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.hide() },
		},
		{
			id:   actionMove,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.Up, k.Nav.Down) },
			run: func(_ host, s panelShared, pressed string) tea.Cmd {
				l.viewer.Move(stepOf(isBound(pressed, s.keys.Nav.Down), 1))

				return nil
			},
		},
		{
			id:   actionNavPage,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Nav.PageUp, k.Nav.PageDown) },
			run: func(_ host, s panelShared, pressed string) tea.Cmd {
				l.viewer.Move(navPageDeltaOf(s.keys, pressed))

				return nil
			},
		},
		{
			id:   actionColumnScroll,
			keys: func(k config.Keybindings) []string { return slices.Concat(k.Column.ScrollDown, k.Column.ScrollUp) },
			run: func(_ host, s panelShared, pressed string) tea.Cmd {
				l.viewer.Move(stepOf(isBound(pressed, s.keys.Column.ScrollDown), 1))

				return nil
			},
		},
		{
			id:   actionLogsBack,
			keys: func(k config.Keybindings) []string { return k.Logs.Back },
			run: func(h host, _ panelShared, _ string) tea.Cmd {
				return h.enter(l.from, screenTarget{agentID: l.agentID})
			},
		},
	}
}

// stepOf is size forward when forward is set and back otherwise.
func stepOf(forward bool, size int) int {
	if forward {
		return size
	}

	return -size
}

func (l *logsScreen) legend() []screenLegendRow {
	return slices.Concat(screenNavLegend(), []screenLegendRow{
		{
			of:    []actionID{actionLogsBack},
			desc:  func(panelShared) string { return "back" },
			strip: true,
		},
	})
}

func (l *logsScreen) help(s panelShared) keyMap {
	return l.actions().help(s)
}
