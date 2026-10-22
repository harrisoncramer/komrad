package ui

import (
	"slices"

	"komrad/config"
	"komrad/internal/ui/keys"

	tea "charm.land/bubbletea/v2"
)

// actions is the agents list as one table and the legend that names it.
func (a *agentsScreen) actions() keys.Spec[*model] {
	return keys.Spec[*model]{
		Table:  a.table(),
		Legend: a.legend(),
	}
}

func (a *agentsScreen) table() []keys.Action[*model] {
	return append(a.modeActions(), a.rowActions()...)
}

// modeActions are the keys that answer whatever the cursor is on.
func (a *agentsScreen) modeActions() []keys.Action[*model] {
	list := a.list()

	return append(list.actions(),
		keys.Action[*model]{
			ID:   actionAgentTriage,
			Keys: func(k config.Keybindings) []string { return k.Agents.Triage },
			When: a.isTriageReachable,
			Nav: func(m *model, _ string) tea.Cmd {
				a.jumpToTriage(m)

				return nil
			},
		},
		keys.Action[*model]{
			ID:   actionTaskNotesBack,
			Keys: keys.FixedBack,
			When: a.isHoveringNotes,
			Nav:  func(m *model, _ string) tea.Cmd { return a.closeNotes(m) },
		},
		keys.Action[*model]{
			ID:   actionAgentsBack,
			Keys: keys.FixedBack,
			When: list.isBrowsing,
			Nav:  func(m *model, _ string) tea.Cmd { return m.pickView() },
		},
	)
}

// rowActions are the keys that act on the row the cursor is on, a workflow or an agent and never
// both.
func (a *agentsScreen) rowActions() []keys.Action[*model] {
	list := a.list()

	return []keys.Action[*model]{
		{
			ID:     actionWorkflowRename,
			Keys:   func(k config.Keybindings) []string { return k.Workflows.Rename },
			When:   a.isHoveringWorkflow,
			Direct: keys.Directly(keys.NotInMenu, func(m *model, _ string) tea.Cmd { return a.openRename(m) }),
		},
		{
			ID:     actionAgentOpen,
			Keys:   keys.FixedOpen,
			When:   list.isBrowsing,
			Direct: keys.Directly("enter is how a person reaches an agent, and the resume is incidental to that", func(m *model, _ string) tea.Cmd { return a.openHovered(m) }),
		},
		{
			ID:     actionAgentRename,
			Keys:   func(k config.Keybindings) []string { return k.Agents.Rename },
			When:   a.isHoveringAgent,
			Direct: keys.Directly(keys.NotInMenu, func(m *model, _ string) tea.Cmd { return a.openRename(m) }),
		},
		{
			ID:     actionTaskRename,
			Keys:   func(k config.Keybindings) []string { return k.Agents.Rename },
			When:   a.isHoveringTask,
			Direct: keys.Directly(keys.NotInMenu, func(m *model, _ string) tea.Cmd { return a.openRename(m) }),
		},
	}
}

func (a *agentsScreen) isBrowsing(m *model) bool {
	return a.list().isBrowsing(m)
}

// isTriageReachable reports whether the triage key has a zone to jump to.
func (a *agentsScreen) isTriageReachable(m *model) bool {
	return a.triage.isOn() && a.isBrowsing(m)
}

func (a *agentsScreen) isHoveringWorkflow(m *model) bool {
	_, ok := a.hoveredWorkflow(m)

	return a.isBrowsing(m) && ok
}

func (a *agentsScreen) isHoveringTask(m *model) bool {
	row, ok := a.hoveredRow(m)

	return a.isBrowsing(m) && ok && row.isTask()
}

// isHoveringNotes reports whether the cursor is on the task whose notes are shown.
func (a *agentsScreen) isHoveringNotes(m *model) bool {
	row, ok := a.hoveredRow(m)

	return a.isBrowsing(m) && ok && a.isShowingNotes(row)
}

func (a *agentsScreen) isHoveringAgent(m *model) bool {
	row, ok := a.hoveredRow(m)

	return a.isBrowsing(m) && (!ok || !row.header)
}

func (a *agentsScreen) legend() []keys.LegendRow[*model] {
	return append(a.list().selectionLegend(), a.browsingLegend()...)
}

// browsingLegend is the ordinary legend, in reading order.
func (a *agentsScreen) browsingLegend() []keys.LegendRow[*model] {
	return slices.Concat([]keys.LegendRow[*model]{
		{
			Of:    []keys.ID{actionAgentOpen},
			Desc:  func(*model) string { return "open" },
			Show:  a.isBrowsing,
			Strip: true,
		},
		{
			Of:   []keys.ID{keys.SelectToggle},
			Desc: a.list().toggleDesc,
			Show: a.isBrowsing,
		},
		{
			Of:   []keys.ID{actionAgentTriage},
			Desc: func(*model) string { return "triage" },
			Show: a.isTriageReachable,
		},
		{
			Of:   []keys.ID{actionWorkflowRename},
			Desc: func(*model) string { return "rename" },
		},
		{
			Of:   []keys.ID{actionAgentRename},
			Desc: func(*model) string { return "rename" },
			Show: a.isHoveringAgent,
		},
		{
			Of:   []keys.ID{actionTaskRename},
			Desc: func(*model) string { return "rename" },
			Show: a.isHoveringTask,
		},
		{
			Of:   []keys.ID{actionTaskNotesBack},
			Desc: func(*model) string { return "close the notes" },
			Show: a.isHoveringNotes,
		},
		{
			Of:   []keys.ID{keys.FilterOpen},
			Desc: filterDescOf,
			Show: a.isBrowsing,
		},
		{
			Of:   []keys.ID{keys.FilterMatch},
			Desc: func(*model) string { return "next/prev match" },
		},
		{
			Of:   []keys.ID{keys.FilterClear},
			Desc: filterClearDescOf,
		},
	}, keys.NavLegend[*model]())
}

func (a *agentsScreen) showHiddenDesc() string {
	if a.showHidden {
		return "put hidden tasks away"
	}

	return "show hidden tasks"
}
