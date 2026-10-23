package logs

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/filter"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/legend"
	"komrad/internal/ui/screen"
)

const (
	actionBack keys.ID = keys.Own + iota
	actionCopy
)

// actions is the logs screen as one table and the legend that names it.
func (l *Screen) actions(in screen.Inputs) keys.Spec[*Screen] {
	if l.IsBlockFocused(in) {
		return l.logActions(in)
	}

	return l.listActions(in)
}

// listActions is the keys on the list of log sources.
func (l *Screen) listActions(in screen.Inputs) keys.Spec[*Screen] {
	return keys.Spec[*Screen]{
		Table:  l.table(in),
		Legend: l.legend(in),
	}
}

func (l *Screen) table(in screen.Inputs) []keys.Action[*Screen] {
	return append(keys.ListActions[*Screen](l.list(), in),
		keys.Action[*Screen]{
			ID:   actionBack,
			Keys: keys.FixedBack,
			Nav: func(*Screen, string) screen.Request {
				return screen.Open{Screen: l.openedFrom, Target: screen.Target{AgentID: l.agentID}}
			},
		},
	)
}

func (l *Screen) legend(in screen.Inputs) []keys.LegendRow[*Screen] {
	return slices.Concat(keys.NavLegend[*Screen](), []keys.LegendRow[*Screen]{
		{
			ActionIDs: []keys.ID{keys.FilterOpen},
			Desc:      keys.FilterDescOf[*Screen](in),
		},
		{
			ActionIDs:     []keys.ID{keys.FilterClear},
			Desc:          keys.FilterClearDescOf[*Screen](in),
			IsInShortHelp: true,
		},
		{
			ActionIDs:     []keys.ID{keys.FilterMatch},
			Desc:          func(*Screen) string { return "next/prev match" },
			IsInShortHelp: true,
		},
		{
			ActionIDs:     []keys.ID{actionBack},
			Desc:          func(*Screen) string { return "back" },
			IsInShortHelp: true,
		},
	})
}

// logActions is the keys on the log once the picker has handed them to it.
func (l *Screen) logActions(in screen.Inputs) keys.Spec[*Screen] {
	return keys.Spec[*Screen]{
		Table: append(keys.NavActions[*Screen](keys.NavActionsParams{Keys: l.deps.Keys, Nav: l, In: in, Page: keys.NavPageStep, Scroll: keys.ColumnWheelStep}), []keys.Action[*Screen]{
			{
				ID:   actionCopy,
				Keys: func(k config.Keybindings) []string { return k.Logs.Copy },
				Nav: func(*Screen, string) screen.Request {
					line, ok := l.viewer.Current()
					if !ok {
						return nil
					}

					return screen.Copy{Text: line}
				},
			},
			{
				ID:   keys.FilterOpen,
				Keys: func(config.Keybindings) []string { return []string{filter.OpenKey} },
				Nav: func(*Screen, string) screen.Request {
					l.openLogFilter()

					return nil
				},
			},
			{
				ID:   actionBack,
				Keys: keys.FixedBack,
				Nav: func(*Screen, string) screen.Request {
					if l.logFilter.IsStanding() {
						return l.clearLogFilter()
					}

					l.logFocus = false

					return nil
				},
			},
			{
				ID:   keys.Quit,
				Keys: keys.FixedQuit,
				Nav:  func(*Screen, string) screen.Request { return screen.Hide{} },
			},
		}...),
		Legend: []keys.LegendRow[*Screen]{
			keys.MoveRow[*Screen](),
			keys.NavPageRow[*Screen](),
			keys.JumpRow[*Screen](),
			{
				ActionIDs: []keys.ID{keys.FilterOpen},
				Desc:      func(*Screen) string { return "filter" },
			},
			{
				ActionIDs:     []keys.ID{actionCopy},
				Desc:          func(*Screen) string { return "copy line" },
				IsInShortHelp: true,
			},
			{
				ActionIDs: []keys.ID{actionBack},
				Desc: func(*Screen) string {
					if l.logFilter.IsStanding() {
						return "clear filter"
					}

					return "back to the list"
				},
				IsInShortHelp: true,
			},
		},
	}
}

func (l *Screen) help(in screen.Inputs) legend.Help {
	return l.actions(in).Help(l, l.deps.Keys)
}
