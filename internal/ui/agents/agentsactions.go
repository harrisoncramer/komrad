package agents

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

const (
	// Back is the agents list's back key, which opens the view picker while the list is browsed.
	Back keys.ID = keys.Own + iota
	actionAgentTriage
	actionAgentOpen
	actionAgentRename
	actionTaskRename
	actionTaskNotesBack
	actionWorkflowRename
	actionArchiveRename
	actionArchiveBack
)

// actions is the agents list as one table and the legend that names it.
func (a *Screen) actions(in screen.Inputs) keys.Spec[*Screen] {
	return keys.Spec[*Screen]{
		Table:  a.table(in),
		Legend: a.legend(in),
	}
}

func (a *Screen) table(in screen.Inputs) []keys.Action[*Screen] {
	return append(a.modeActions(in), a.rowActions(in)...)
}

// modeActions are the keys that answer whatever the cursor is on.
func (a *Screen) modeActions(in screen.Inputs) []keys.Action[*Screen] {
	return append(keys.ListActions[*Screen](a.list(), in),
		keys.Action[*Screen]{
			ID:   actionAgentTriage,
			Keys: func(k config.Keybindings) []string { return k.Agents.Triage },
			When: (*Screen).isTriageReachable,
			Nav:  func(*Screen, string) screen.Request { return a.jumpToTriage(in) },
		},
		keys.Action[*Screen]{
			ID:   actionTaskNotesBack,
			Keys: keys.FixedBack,
			When: func(*Screen) bool { return a.isHoveringNotes(in) },
			Nav:  func(*Screen, string) screen.Request { return a.closeNotes() },
		},
		keys.Action[*Screen]{
			ID:   Back,
			Keys: keys.FixedBack,
			When: (*Screen).isBrowsing,
			Nav:  func(*Screen, string) screen.Request { return screen.PickView{} },
		},
	)
}

// rowActions are the keys that act on the row the cursor is on, a workflow or an agent and never
// both.
func (a *Screen) rowActions(in screen.Inputs) []keys.Action[*Screen] {
	return []keys.Action[*Screen]{
		{
			ID:     actionWorkflowRename,
			Keys:   func(k config.Keybindings) []string { return k.Workflows.Rename },
			When:   func(*Screen) bool { return a.isHoveringWorkflow(in) },
			Direct: keys.Directly(keys.NotInMenu, func(*Screen, string) screen.Request { return a.openRename(in) }),
		},
		{
			ID:     actionAgentOpen,
			Keys:   keys.FixedOpen,
			When:   (*Screen).isBrowsing,
			Direct: keys.Directly("enter is how a person reaches an agent, and the resume is incidental to that", func(*Screen, string) screen.Request { return a.OpenHovered(in) }),
		},
		{
			ID:     actionAgentRename,
			Keys:   func(k config.Keybindings) []string { return k.Agents.Rename },
			When:   func(*Screen) bool { return a.isHoveringAgent(in) },
			Direct: keys.Directly(keys.NotInMenu, func(*Screen, string) screen.Request { return a.openRename(in) }),
		},
		{
			ID:     actionTaskRename,
			Keys:   func(k config.Keybindings) []string { return k.Agents.Rename },
			When:   func(*Screen) bool { return a.isHoveringTask(in) },
			Direct: keys.Directly(keys.NotInMenu, func(*Screen, string) screen.Request { return a.openRename(in) }),
		},
	}
}

func (a *Screen) isBrowsing() bool {
	return a.list().IsBrowsing()
}

// isTriageReachable reports whether the triage key has a zone to jump to.
func (a *Screen) isTriageReachable() bool {
	return a.triage.isOn() && a.isBrowsing()
}

func (a *Screen) isHoveringWorkflow(in screen.Inputs) bool {
	_, ok := a.hoveredWorkflow(in)

	return a.isBrowsing() && ok
}

func (a *Screen) isHoveringTask(in screen.Inputs) bool {
	row, ok := a.hoveredRow(in)

	return a.isBrowsing() && ok && row.isTask()
}

// isHoveringNotes reports whether the cursor is on the task whose notes are shown.
func (a *Screen) isHoveringNotes(in screen.Inputs) bool {
	row, ok := a.hoveredRow(in)

	return a.isBrowsing() && ok && a.isShowingNotes(row)
}

func (a *Screen) isHoveringAgent(in screen.Inputs) bool {
	row, ok := a.hoveredRow(in)

	return a.isBrowsing() && (!ok || !row.header)
}

func (a *Screen) legend(in screen.Inputs) []keys.LegendRow[*Screen] {
	return append(keys.SelectionLegend[*Screen](a.list(), in), a.browsingLegend(in)...)
}

// browsingLegend is the ordinary legend, in reading order.
func (a *Screen) browsingLegend(in screen.Inputs) []keys.LegendRow[*Screen] {
	return slices.Concat([]keys.LegendRow[*Screen]{
		{
			ActionIDs: []keys.ID{actionAgentOpen},
			Desc:      func(*Screen) string { return "open" },
			Show:      (*Screen).isBrowsing,
			Strip:     true,
		},
		{
			ActionIDs: []keys.ID{keys.SelectToggle},
			Desc:      func(*Screen) string { return a.list().ToggleDesc(in) },
			Show:      (*Screen).isBrowsing,
		},
		{
			ActionIDs: []keys.ID{actionAgentTriage},
			Desc:      func(*Screen) string { return "triage" },
			Show:      (*Screen).isTriageReachable,
		},
		{
			ActionIDs: []keys.ID{actionWorkflowRename},
			Desc:      func(*Screen) string { return "rename" },
		},
		{
			ActionIDs: []keys.ID{actionAgentRename},
			Desc:      func(*Screen) string { return "rename" },
			Show:      func(*Screen) bool { return a.isHoveringAgent(in) },
		},
		{
			ActionIDs: []keys.ID{actionTaskRename},
			Desc:      func(*Screen) string { return "rename" },
			Show:      func(*Screen) bool { return a.isHoveringTask(in) },
		},
		{
			ActionIDs: []keys.ID{actionTaskNotesBack},
			Desc:      func(*Screen) string { return "close the notes" },
			Show:      func(*Screen) bool { return a.isHoveringNotes(in) },
		},
		{
			ActionIDs: []keys.ID{keys.FilterOpen},
			Desc:      keys.FilterDescOf[*Screen](in),
			Show:      (*Screen).isBrowsing,
		},
		{
			ActionIDs: []keys.ID{keys.FilterMatch},
			Desc:      func(*Screen) string { return "next/prev match" },
		},
		{
			ActionIDs: []keys.ID{keys.FilterClear},
			Desc:      keys.FilterClearDescOf[*Screen](in),
		},
	}, keys.NavLegend[*Screen]())
}

func (a *Screen) showHiddenDesc() string {
	if a.showHidden {
		return "put hidden tasks away"
	}

	return "show hidden tasks"
}
