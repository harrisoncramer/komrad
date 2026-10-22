package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// actions is the logs screen as one table and the legend that names it.
func (l *logsScreen) actions(m *model) keys.Spec[*model] {
	if l.isBlockFocused(m) {
		return l.logActions()
	}

	return l.listActions()
}

// listActions is the keys on the list of log sources.
func (l *logsScreen) listActions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table:  l.table(),
		Legend: l.legend(),
	}
}

func (l *logsScreen) table() []keys.Action[*model] {
	return append(l.list().actions(),
		keys.Action[*model]{
			ID:   actionLogsBack,
			Keys: keys.FixedBack,
			Nav: func(m *model, _ string) tea.Cmd {
				return m.apply(screen.Open{Screen: l.openedFrom, Target: screen.Target{AgentID: l.agentID}})
			},
		},
	)
}

func (l *logsScreen) legend() []keys.LegendRow[*model] {
	return slices.Concat(keys.NavLegend[*model](), []keys.LegendRow[*model]{
		{
			Of:   []keys.ID{keys.FilterOpen},
			Desc: filterDescOf,
		},
		{
			Of:    []keys.ID{keys.FilterClear},
			Desc:  filterClearDescOf,
			Strip: true,
		},
		{
			Of:    []keys.ID{keys.FilterMatch},
			Desc:  func(*model) string { return "next/prev match" },
			Strip: true,
		},
		{
			Of:    []keys.ID{actionLogsBack},
			Desc:  func(*model) string { return "back" },
			Strip: true,
		},
	})
}

// logActions is the keys on the log once the picker has handed them to it.
func (l *logsScreen) logActions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table: append(navActions(l, navSteps{page: navPageStep, scroll: columnWheelStep}), []keys.Action[*model]{
			{
				ID:   actionLogsCopy,
				Keys: func(k config.Keybindings) []string { return k.Logs.Copy },
				Nav: func(m *model, _ string) tea.Cmd {
					line, ok := l.viewer.Current()
					if !ok {
						return nil
					}

					return m.copyCmd(line)
				},
			},
			{
				ID:   keys.FilterOpen,
				Keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				Nav: func(*model, string) tea.Cmd {
					l.openLogFilter()

					return nil
				},
			},
			{
				ID:   actionLogsBack,
				Keys: keys.FixedBack,
				Nav: func(m *model, _ string) tea.Cmd {
					if l.logFilter.isStanding() {
						return l.clearLogFilter(m)
					}

					l.logFocus = false

					return nil
				},
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(m *model, _ string) tea.Cmd { return m.apply(screen.Hide{}) },
			},
		}...),
		Legend: []keys.LegendRow[*model]{
			keys.MoveRow[*model](),
			keys.NavPageRow[*model](),
			keys.JumpRow[*model](),
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: func(*model) string { return "filter" },
			},
			{
				Of:    []keys.ID{actionLogsCopy},
				Desc:  func(*model) string { return "copy line" },
				Strip: true,
			},
			{
				Of: []keys.ID{actionLogsBack},
				Desc: func(*model) string {
					if l.logFilter.isStanding() {
						return "clear filter"
					}

					return "back to the list"
				},
				Strip: true,
			},
		},
	}
}

func (l *logsScreen) help(m *model) keyMap {
	return m.helpOf(l.actions(m))
}
