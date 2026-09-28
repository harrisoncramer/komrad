package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the logs screen as one table and the legend that names it.
func (l *logsScreen) actions(s panelShared) screenActionSpec {
	if l.isLogFocused(s) {
		return l.logActions()
	}

	return l.listActions()
}

// listActions is the keys on the list of log sources.
func (l *logsScreen) listActions() screenActionSpec {
	return screenActionSpec{
		table:  l.table(),
		legend: l.legend(),
	}
}

func (l *logsScreen) table() []screenAction {
	return append(l.list().actions(),
		screenAction{
			id:   actionLogsBack,
			keys: func(k config.Keybindings) []string { return k.Logs.Back },
			run: func(h host, _ panelShared, _ string) tea.Cmd {
				return h.enter(l.from, screenTarget{agentID: l.agentID})
			},
		},
	)
}

func (l *logsScreen) legend() []screenLegendRow {
	return slices.Concat(screenNavLegend(), []screenLegendRow{
		{
			of:   []actionID{actionFilterOpen},
			desc: filterDescOf,
		},
		{
			of:    []actionID{actionFilterClear},
			desc:  filterClearDescOf,
			strip: true,
		},
		{
			of:    []actionID{actionFilterMatch},
			desc:  func(panelShared) string { return "next/prev match" },
			strip: true,
		},
		{
			of:    []actionID{actionLogsBack},
			desc:  func(panelShared) string { return "back" },
			strip: true,
		},
	})
}

// logActions is the keys on the log once the cycle key has handed them to it.
func (l *logsScreen) logActions() screenActionSpec {
	return screenActionSpec{
		table: []screenAction{
			{
				id:   actionMove,
				keys: func(b config.Keybindings) []string { return slices.Concat(b.Nav.Up, b.Nav.Down) },
				run: func(_ host, s panelShared, pressed string) tea.Cmd {
					return l.move(directionOf(pressed, s.keys.Nav.Up))
				},
			},
			{
				id:   actionNavPage,
				keys: func(b config.Keybindings) []string { return slices.Concat(b.Nav.PageUp, b.Nav.PageDown) },
				run: func(_ host, s panelShared, pressed string) tea.Cmd {
					return l.move(directionOf(pressed, s.keys.Nav.PageUp) * navPageStep)
				},
			},
			{
				id:   actionColumnScroll,
				keys: func(b config.Keybindings) []string { return slices.Concat(b.Column.ScrollDown, b.Column.ScrollUp) },
				run: func(_ host, s panelShared, pressed string) tea.Cmd {
					return l.move(directionOf(pressed, s.keys.Column.ScrollUp) * columnWheelStep)
				},
			},
			{
				id:   actionJump,
				keys: func(b config.Keybindings) []string { return slices.Concat(b.Nav.Top, b.Nav.Bottom) },
				run: func(_ host, s panelShared, pressed string) tea.Cmd {
					if isBound(pressed, s.keys.Nav.Bottom) {
						return l.move(l.viewer.Len())
					}

					return l.move(-l.viewer.Len())
				},
			},
			{
				id:   actionLogsBack,
				keys: func(k config.Keybindings) []string { return k.Logs.Back },
				run: func(host, panelShared, string) tea.Cmd {
					l.logFocus = false

					return nil
				},
			},
			{
				id:   actionQuit,
				keys: func(k config.Keybindings) []string { return k.Logs.Quit },
				run:  func(h host, _ panelShared, _ string) tea.Cmd { return h.hide() },
			},
		},
		legend: []screenLegendRow{
			diffMoveRow(),
			diffPageRow(),
			diffJumpRow(),
			{
				of:    []actionID{actionLogsBack},
				desc:  func(panelShared) string { return "back to the list" },
				strip: true,
			},
		},
	}
}

func (l *logsScreen) help(s panelShared) keyMap {
	return l.actions(s).help(s)
}
