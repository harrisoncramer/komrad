package ui

import (
	"errors"
	"fmt"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// archiveMenuChoice is one entry of the archive menu.
type archiveMenuChoice int

const (
	archiveMenuRestore archiveMenuChoice = iota
	archiveMenuRestoreOpen
	archiveMenuDelete
)

var errNeverStarted = errors.New("this agent never got as far as starting, so there is no conversation to open")

// archived is what the archive menu acts on: chosen or hovered agents, a workflow, or a schedule.
type archived struct {
	agents   []panel.Agent
	workflow *panel.Workflow
	schedule *panel.Schedule
	chosen   bool
}

// name is what the menu's band is titled with.
func (t archived) name(l listing) string {
	switch {
	case t.schedule != nil:
		return t.schedule.Name
	case t.workflow != nil:
		return t.workflow.Name
	case t.chosen:
		return describeAgents(t.agents)
	}

	return l.agentName(t.agents[0])
}

// opensAgent is the one agent whose conversation can be put back in a pane.
func (t archived) opensAgent() (panel.Agent, bool) {
	if t.chosen || len(t.agents) != 1 || !t.agents[0].State.DoesPaneDecide() {
		return panel.Agent{}, false
	}

	return t.agents[0], true
}

// menuTarget is what the archive menu would act on, and none on the folder of a live workflow.
func (a *archiveScreen) menuTarget(s panelShared) (archived, bool) {
	if a.onSchedules(s) {
		schedule, ok := a.hoveredSchedule(s)

		return archived{schedule: &schedule}, ok
	}

	if a.selected.isActive() {
		agents := chosenAgents(a.selected, a.listRows(s))

		return archived{agents: agents, chosen: true}, len(agents) > 0
	}

	if workflow, ok := a.hoveredWorkflow(s); ok {
		return archived{workflow: &workflow}, true
	}

	agent, ok := a.hoveredAgent(s)

	return archived{agents: []panel.Agent{agent}}, ok
}

// archiveMenu is the archive's actions on a row, opened from its menu.
func archiveMenu(s panelShared, target archived) submenu {
	restore, remove := "restore", "delete for good"
	if target.chosen {
		restore, remove = "restore all", "delete all for good"
	}

	_, opens := target.opensAgent()

	return submenu{
		kind:  formArchiveMenu,
		title: "archive",
		crumb: "archive",
		entries: []submenuEntry{
			{label: restore, keys: s.keys.ArchiveMenu.Restore},
			{label: "restore and open", keys: s.keys.ArchiveMenu.RestoreOpen, isOff: !opens},
			{label: remove, keys: s.keys.ArchiveMenu.Delete},
		},
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			return m.runArchiveChoice(target, archiveMenuChoice(choice))
		},
	}
}

// runArchiveChoice does what the entry asked for, or says why the row is not one to ask it of.
func (m model) runArchiveChoice(target archived, choice archiveMenuChoice) (tea.Model, tea.Cmd) {
	switch choice {
	case archiveMenuRestore:
		return m.closeMenuThen(func(h host) tea.Cmd { return restoreArchived(h, target) })
	case archiveMenuRestoreOpen:
		agent, ok := target.opensAgent()
		if !ok {
			return m.refuseSubpage("Could not restore and open "+target.name(m.listing), errNeverStarted)
		}

		return m.closeMenuThen(func(h host) tea.Cmd { return restoreAndResumeCmd(h, agent) })
	case archiveMenuDelete:
		m.leaveForm()
		cmd := confirmDeleteArchivedRow(&m, m.shared(), target)

		return m, cmd
	}

	return m.closeForm()
}

func restoreArchived(h host, target archived) tea.Cmd {
	switch {
	case target.schedule != nil:
		schedule := *target.schedule

		return h.writeCmd(
			fmt.Sprintf("Could not restore schedule %q", schedule.Name),
			func(c api) error { return c.restoreSchedule(schedule.ID) },
		)
	case target.workflow != nil:
		workflow := *target.workflow

		return h.writeCmd(
			fmt.Sprintf("Could not restore workflow %q", workflow.Name),
			func(c api) error { return c.restoreWorkflow(workflow.ID) },
		)
	}

	return restoreAgentsCmd(h, target.agents)
}

func confirmDeleteArchivedRow(h host, s panelShared, target archived) tea.Cmd {
	switch {
	case target.schedule != nil:
		return confirmDeleteSchedule(h, *target.schedule)
	case target.workflow != nil:
		return confirmDeleteWorkflow(h, s, *target.workflow)
	}

	return confirmDeleteArchived(h, s, target.agents)
}

// closeMenuThen takes the menu down and runs what was picked over the screen it was opened on.
func (m model) closeMenuThen(run func(host) tea.Cmd) (tea.Model, tea.Cmd) {
	closed, back := m.closeForm()
	next, _ := closed.(model)
	cmd := run(&next)

	return next, tea.Batch(back, cmd)
}
