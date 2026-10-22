package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"
)

// actions is the agents list as one table and the legend that names it.
func (a *agentsScreen) actions(in screen.Inputs) keys.Spec[*agentsScreen] {
	return keys.Spec[*agentsScreen]{
		Table:  a.table(in),
		Legend: a.legend(in),
	}
}

func (a *agentsScreen) table(in screen.Inputs) []keys.Action[*agentsScreen] {
	return append(a.modeActions(in), a.rowActions(in)...)
}

// modeActions are the keys that answer whatever the cursor is on.
func (a *agentsScreen) modeActions(in screen.Inputs) []keys.Action[*agentsScreen] {
	return append(listActions[*agentsScreen](a.list(), in),
		keys.Action[*agentsScreen]{
			ID:   actionAgentTriage,
			Keys: func(k config.Keybindings) []string { return k.Agents.Triage },
			When: (*agentsScreen).isTriageReachable,
			Nav:  func(*agentsScreen, string) screen.Request { return a.jumpToTriage(in) },
		},
		keys.Action[*agentsScreen]{
			ID:   actionTaskNotesBack,
			Keys: keys.FixedBack,
			When: func(*agentsScreen) bool { return a.isHoveringNotes(in) },
			Nav:  func(*agentsScreen, string) screen.Request { return a.closeNotes() },
		},
		keys.Action[*agentsScreen]{
			ID:   actionAgentsBack,
			Keys: keys.FixedBack,
			When: (*agentsScreen).isBrowsing,
			Nav:  func(*agentsScreen, string) screen.Request { return screen.PickView{} },
		},
	)
}

// rowActions are the keys that act on the row the cursor is on, a workflow or an agent and never
// both.
func (a *agentsScreen) rowActions(in screen.Inputs) []keys.Action[*agentsScreen] {
	return []keys.Action[*agentsScreen]{
		{
			ID:     actionWorkflowRename,
			Keys:   func(k config.Keybindings) []string { return k.Workflows.Rename },
			When:   func(*agentsScreen) bool { return a.isHoveringWorkflow(in) },
			Direct: keys.Directly(keys.NotInMenu, func(*agentsScreen, string) screen.Request { return a.openRename(in) }),
		},
		{
			ID:     actionAgentOpen,
			Keys:   keys.FixedOpen,
			When:   (*agentsScreen).isBrowsing,
			Direct: keys.Directly("enter is how a person reaches an agent, and the resume is incidental to that", func(*agentsScreen, string) screen.Request { return a.openHovered(in) }),
		},
		{
			ID:     actionAgentRename,
			Keys:   func(k config.Keybindings) []string { return k.Agents.Rename },
			When:   func(*agentsScreen) bool { return a.isHoveringAgent(in) },
			Direct: keys.Directly(keys.NotInMenu, func(*agentsScreen, string) screen.Request { return a.openRename(in) }),
		},
		{
			ID:     actionTaskRename,
			Keys:   func(k config.Keybindings) []string { return k.Agents.Rename },
			When:   func(*agentsScreen) bool { return a.isHoveringTask(in) },
			Direct: keys.Directly(keys.NotInMenu, func(*agentsScreen, string) screen.Request { return a.openRename(in) }),
		},
	}
}

func (a *agentsScreen) isBrowsing() bool {
	return a.list().isBrowsing()
}

// isTriageReachable reports whether the triage key has a zone to jump to.
func (a *agentsScreen) isTriageReachable() bool {
	return a.triage.isOn() && a.isBrowsing()
}

func (a *agentsScreen) isHoveringWorkflow(in screen.Inputs) bool {
	_, ok := a.hoveredWorkflow(in)

	return a.isBrowsing() && ok
}

func (a *agentsScreen) isHoveringTask(in screen.Inputs) bool {
	row, ok := a.hoveredRow(in)

	return a.isBrowsing() && ok && row.isTask()
}

// isHoveringNotes reports whether the cursor is on the task whose notes are shown.
func (a *agentsScreen) isHoveringNotes(in screen.Inputs) bool {
	row, ok := a.hoveredRow(in)

	return a.isBrowsing() && ok && a.isShowingNotes(row)
}

func (a *agentsScreen) isHoveringAgent(in screen.Inputs) bool {
	row, ok := a.hoveredRow(in)

	return a.isBrowsing() && (!ok || !row.header)
}

func (a *agentsScreen) legend(in screen.Inputs) []keys.LegendRow[*agentsScreen] {
	return append(selectionLegend[*agentsScreen](a.list(), in), a.browsingLegend(in)...)
}

// browsingLegend is the ordinary legend, in reading order.
func (a *agentsScreen) browsingLegend(in screen.Inputs) []keys.LegendRow[*agentsScreen] {
	return slices.Concat([]keys.LegendRow[*agentsScreen]{
		{
			Of:    []keys.ID{actionAgentOpen},
			Desc:  func(*agentsScreen) string { return "open" },
			Show:  (*agentsScreen).isBrowsing,
			Strip: true,
		},
		{
			Of:   []keys.ID{keys.SelectToggle},
			Desc: func(*agentsScreen) string { return a.list().toggleDesc(in) },
			Show: (*agentsScreen).isBrowsing,
		},
		{
			Of:   []keys.ID{actionAgentTriage},
			Desc: func(*agentsScreen) string { return "triage" },
			Show: (*agentsScreen).isTriageReachable,
		},
		{
			Of:   []keys.ID{actionWorkflowRename},
			Desc: func(*agentsScreen) string { return "rename" },
		},
		{
			Of:   []keys.ID{actionAgentRename},
			Desc: func(*agentsScreen) string { return "rename" },
			Show: func(*agentsScreen) bool { return a.isHoveringAgent(in) },
		},
		{
			Of:   []keys.ID{actionTaskRename},
			Desc: func(*agentsScreen) string { return "rename" },
			Show: func(*agentsScreen) bool { return a.isHoveringTask(in) },
		},
		{
			Of:   []keys.ID{actionTaskNotesBack},
			Desc: func(*agentsScreen) string { return "close the notes" },
			Show: func(*agentsScreen) bool { return a.isHoveringNotes(in) },
		},
		{
			Of:   []keys.ID{keys.FilterOpen},
			Desc: filterDescOf[*agentsScreen](in),
			Show: (*agentsScreen).isBrowsing,
		},
		{
			Of:   []keys.ID{keys.FilterMatch},
			Desc: func(*agentsScreen) string { return "next/prev match" },
		},
		{
			Of:   []keys.ID{keys.FilterClear},
			Desc: filterClearDescOf[*agentsScreen](in),
		},
	}, keys.NavLegend[*agentsScreen]())
}

func (a *agentsScreen) showHiddenDesc() string {
	if a.showHidden {
		return "put hidden tasks away"
	}

	return "show hidden tasks"
}
