package ui

import (
	"slices"

	"komrad/config"

	tea "charm.land/bubbletea/v2"
)

// actions is the logs screen as one table and the legend that names it.
func (l *logsScreen) actions(m *model) screenActionSpec {
	if l.isBlockFocused(m) {
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
			keys: fixedBack,
			nav: func(m *model, _ string) tea.Cmd {
				return m.enter(l.from, screenTarget{agentID: l.agentID})
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
			desc:  func(*model) string { return "next/prev match" },
			strip: true,
		},
		{
			of:    []actionID{actionLogsBack},
			desc:  func(*model) string { return "back" },
			strip: true,
		},
	})
}

// logActions is the keys on the log once the picker has handed them to it.
func (l *logsScreen) logActions() screenActionSpec {
	return screenActionSpec{
		table: append(navActions(l, navSteps{page: navPageStep, scroll: columnWheelStep}), []screenAction{
			{
				id:   actionLogsCopy,
				keys: func(k config.Keybindings) []string { return k.Logs.Copy },
				nav: func(m *model, _ string) tea.Cmd {
					line, ok := l.viewer.Current()
					if !ok {
						return nil
					}

					return m.copyCmd(line)
				},
			},
			{
				id:   actionFilterOpen,
				keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				nav: func(*model, string) tea.Cmd {
					l.openLogFilter()

					return nil
				},
			},
			{
				id:   actionLogsBack,
				keys: fixedBack,
				nav: func(m *model, _ string) tea.Cmd {
					if l.logFilter.isStanding() {
						return l.clearLogFilter(m)
					}

					l.logFocus = false

					return nil
				},
			},
			{
				id:   actionQuit,
				keys: fixedQuit,
				nav:  func(m *model, _ string) tea.Cmd { return m.hide() },
			},
		}...),
		legend: []screenLegendRow{
			navMoveRow("move"),
			navPageRow(),
			navJumpRow(),
			{
				of:   []actionID{actionFilterOpen},
				desc: func(*model) string { return "filter" },
			},
			{
				of:    []actionID{actionLogsCopy},
				desc:  func(*model) string { return "copy line" },
				strip: true,
			},
			{
				of: []actionID{actionLogsBack},
				desc: func(*model) string {
					if l.logFilter.isStanding() {
						return "clear filter"
					}

					return "back to the list"
				},
				strip: true,
			},
		},
	}
}

func (l *logsScreen) help(m *model) keyMap {
	return l.actions(m).help(m)
}
