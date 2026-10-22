package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/screen"
)

// actions is the logs screen as one table and the legend that names it.
func (l *logsScreen) actions(in screen.Inputs) keys.Spec[*logsScreen] {
	if l.isBlockFocused(in) {
		return l.logActions(in)
	}

	return l.listActions(in)
}

// listActions is the keys on the list of log sources.
func (l *logsScreen) listActions(in screen.Inputs) keys.Spec[*logsScreen] {
	return keys.Spec[*logsScreen]{
		Table:  l.table(in),
		Legend: l.legend(in),
	}
}

func (l *logsScreen) table(in screen.Inputs) []keys.Action[*logsScreen] {
	return append(listActions[*logsScreen](l.list(), in),
		keys.Action[*logsScreen]{
			ID:   actionLogsBack,
			Keys: keys.FixedBack,
			Nav: func(*logsScreen, string) screen.Request {
				return screen.Open{Screen: l.openedFrom, Target: screen.Target{AgentID: l.agentID}}
			},
		},
	)
}

func (l *logsScreen) legend(in screen.Inputs) []keys.LegendRow[*logsScreen] {
	return slices.Concat(keys.NavLegend[*logsScreen](), []keys.LegendRow[*logsScreen]{
		{
			Of:   []keys.ID{keys.FilterOpen},
			Desc: filterDescOf[*logsScreen](in),
		},
		{
			Of:    []keys.ID{keys.FilterClear},
			Desc:  filterClearDescOf[*logsScreen](in),
			Strip: true,
		},
		{
			Of:    []keys.ID{keys.FilterMatch},
			Desc:  func(*logsScreen) string { return "next/prev match" },
			Strip: true,
		},
		{
			Of:    []keys.ID{actionLogsBack},
			Desc:  func(*logsScreen) string { return "back" },
			Strip: true,
		},
	})
}

// logActions is the keys on the log once the picker has handed them to it.
func (l *logsScreen) logActions(in screen.Inputs) keys.Spec[*logsScreen] {
	return keys.Spec[*logsScreen]{
		Table: append(navActions[*logsScreen](l.deps.keys, l, in, navSteps{page: navPageStep, scroll: columnWheelStep}), []keys.Action[*logsScreen]{
			{
				ID:   actionLogsCopy,
				Keys: func(k config.Keybindings) []string { return k.Logs.Copy },
				Nav: func(*logsScreen, string) screen.Request {
					line, ok := l.viewer.Current()
					if !ok {
						return nil
					}

					return screen.Copy{Text: line}
				},
			},
			{
				ID:   keys.FilterOpen,
				Keys: func(config.Keybindings) []string { return []string{filterOpenKey} },
				Nav: func(*logsScreen, string) screen.Request {
					l.openLogFilter()

					return nil
				},
			},
			{
				ID:   actionLogsBack,
				Keys: keys.FixedBack,
				Nav: func(*logsScreen, string) screen.Request {
					if l.logFilter.isStanding() {
						return l.clearLogFilter()
					}

					l.logFocus = false

					return nil
				},
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(*logsScreen, string) screen.Request { return screen.Hide{} },
			},
		}...),
		Legend: []keys.LegendRow[*logsScreen]{
			keys.MoveRow[*logsScreen](),
			keys.NavPageRow[*logsScreen](),
			keys.JumpRow[*logsScreen](),
			{
				Of:   []keys.ID{keys.FilterOpen},
				Desc: func(*logsScreen) string { return "filter" },
			},
			{
				Of:    []keys.ID{actionLogsCopy},
				Desc:  func(*logsScreen) string { return "copy line" },
				Strip: true,
			},
			{
				Of: []keys.ID{actionLogsBack},
				Desc: func(*logsScreen) string {
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

func (l *logsScreen) help(in screen.Inputs) legend.Help {
	return l.actions(in).Help(l, l.deps.keys)
}
