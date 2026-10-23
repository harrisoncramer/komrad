package agents

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

// actions is the archived agents as one table and the legend that names it.
func (a Archive) actions(in screen.Inputs) keys.Spec[Archive] {
	return keys.Spec[Archive]{
		Table:  a.table(in),
		Legend: a.legend(in),
	}
}

func (a Archive) table(in screen.Inputs) []keys.Action[Archive] {
	list := a.list()
	browsing := func(Archive) bool { return list.IsBrowsing() }

	return append(keys.ListActions[Archive](list, in),
		keys.Action[Archive]{
			ID:   actionArchiveBack,
			Keys: keys.FixedBack,
			When: browsing,
			Nav:  func(Archive, string) screen.Request { return screen.FocusViewSwitcher{} },
		},
		keys.Action[Archive]{
			ID:     actionArchiveRename,
			Keys:   func(k config.Keybindings) []string { return k.Archive.Rename },
			When:   browsing,
			Direct: keys.Directly(keys.NotInMenu, func(Archive, string) screen.Request { return a.openRename(in) }),
		},
	)
}

func (a Archive) legend(in screen.Inputs) []keys.LegendRow[Archive] {
	list := a.list()

	return slices.Concat(keys.SelectionLegend[Archive](list, in), []keys.LegendRow[Archive]{
		{
			ActionIDs:     []keys.ID{actionArchiveRename},
			Desc:          func(Archive) string { return "rename" },
			Show:          func(Archive) bool { return a.holdsARenameable(in) },
			IsInShortHelp: true,
		},
		{
			ActionIDs: []keys.ID{keys.SelectToggle},
			Desc:      func(Archive) string { return list.ToggleDesc(in) },
			Show:      func(Archive) bool { return a.holdsAChoosable(in) },
		},
		{
			ActionIDs: []keys.ID{keys.FilterOpen},
			Desc:      keys.FilterDescOf[Archive](in),
		},
	}, keys.NavLegend[Archive](), []keys.LegendRow[Archive]{
		{
			ActionIDs: []keys.ID{actionArchiveBack},
			Desc:      func(Archive) string { return "back" },
		},
		{
			ActionIDs:     []keys.ID{keys.FilterClear},
			Desc:          keys.FilterClearDescOf[Archive](in),
			IsInShortHelp: true,
		},
		{
			ActionIDs:     []keys.ID{keys.FilterMatch},
			Desc:          func(Archive) string { return "next/prev match" },
			IsInShortHelp: true,
		},
	})
}

// holdsAChoosable reports whether the cursor is on a row while no selection is active.
func (a *archivedAgents) holdsAChoosable(in screen.Inputs) bool {
	if a.Selected.IsActive() {
		return false
	}

	_, ok := list.At(a.listRows(in), a.Row)

	return ok
}

// holdsARenameable reports whether the cursor sits on a row the user can rename.
func (a *archivedAgents) holdsARenameable(in screen.Inputs) bool {
	_, ok := a.renameable(in)

	return ok
}

// holdsAnAgent reports whether the cursor is on an archived agent rather than on the folder
// holding one.
func (a *archivedAgents) holdsAnAgent(in screen.Inputs) bool {
	if a.Selected.IsActive() {
		return false
	}

	_, ok := a.hoveredAgent(in)

	return ok
}

// HoveredAgentID is the archived agent under the cursor, empty on a folder.
func (a *archivedAgents) HoveredAgentID(in screen.Inputs) string {
	agent, ok := a.hoveredAgent(in)
	if !ok {
		return ""
	}

	return agent.ID
}

// RecordAgent is the agent the record under the cursor is about, which for a task is its writer.
func (a *archivedAgents) RecordAgent(in screen.Inputs) (panel.AgentRow, bool) {
	row, ok := a.hoveredRow(in)
	if !ok {
		return panel.AgentRow{}, false
	}

	return row.recordAgent(in.Listing)
}

// RecordTask is the task whose checkout the record under the cursor shows.
func (a *archivedAgents) RecordTask(in screen.Inputs) (panel.Task, bool) {
	row, ok := a.hoveredRow(in)
	if !ok {
		return panel.Task{}, false
	}

	return row.recordTask(in.Listing)
}

// Resting is never an agent on the archive.
func (a *archivedAgents) Resting(screen.Inputs) (panel.AgentRow, bool) {
	return panel.AgentRow{}, false
}
