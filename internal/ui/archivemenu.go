package ui

import (
	"errors"
	"fmt"

	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

// archiveMenuChoice is one entry of the archive menu.
type archiveMenuChoice int

const (
	archiveMenuRestore archiveMenuChoice = iota
	archiveMenuRestoreOpen
	archiveMenuDelete
)

var (
	errNeverStarted  = errors.New("this agent never got as far as starting, so there is no conversation to open")
	errOpensOneAgent = errors.New("restore and open puts one agent's conversation back in a pane, so it needs a single agent")
)

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

// opensAgent is the one agent whose conversation can be put back in a pane, or why there is none.
func (t archived) opensAgent() (*panel.Agent, error) {
	if t.chosen || len(t.agents) != 1 {
		return nil, errOpensOneAgent
	}

	if !t.agents[0].State.DoesPaneDecide() {
		return nil, errNeverStarted
	}

	return &t.agents[0], nil
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

	_, err := target.opensAgent()

	return submenu{
		kind:  formArchiveMenu,
		title: "archive",
		crumb: "archive",
		entries: []submenuEntry{
			{label: restore, keys: s.keys.ArchiveMenu.Restore},
			{label: "restore and open", keys: s.keys.ArchiveMenu.RestoreOpen, isOff: err != nil},
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
		agent, err := target.opensAgent()
		if err != nil {
			return m.refuseSubpage("Could not restore and open "+target.name(m.listing), err)
		}

		return m.closeMenuThen(func(h host) tea.Cmd { return restoreAndResumeCmd(h, *agent) })
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
			"restoring "+schedule.Name,
			fmt.Sprintf("Could not restore schedule %q", schedule.Name),
			func(c api) error { return c.restoreSchedule(schedule.ID) },
		)
	case target.workflow != nil:
		workflow := *target.workflow

		return h.writeCmd(
			"restoring "+workflow.Name,
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

// liveArchive is one entry of the archive menu on a row that is not archived yet.
type liveArchive struct {
	label string
	keys  []string
	run   func(m model) (tea.Model, tea.Cmd)
}

// liveArchiveMenu is the archive menu on a row that is not archived yet.
func liveArchiveMenu(entries ...liveArchive) submenu {
	return submenu{
		kind:  formArchiveMenu,
		title: "archive",
		crumb: "archive",
		entries: xslice.Map(entries, func(entry liveArchive) submenuEntry {
			return submenuEntry{label: entry.label, keys: entry.keys}
		}),
		answer: func(m model, choice int) (tea.Model, tea.Cmd) {
			return entries[choice].run(m)
		},
	}
}

func agentsArchiveMenu(s panelShared, agents []panel.Agent, chosen bool) submenu {
	archive, remove := "archive", "archive and delete for good"
	if chosen {
		archive, remove = "archive all", "archive and delete all for good"
	}

	return liveArchiveMenu(
		liveArchive{
			label: archive,
			keys:  s.keys.ArchiveMenu.Archive,
			run: func(m model) (tea.Model, tea.Cmd) {
				return m.closeMenuWith(func(next model) (model, tea.Cmd) { return next.archiveAgents(agents) })
			},
		},
		liveArchive{
			label: remove,
			keys:  s.keys.ArchiveMenu.Delete,
			run: func(m model) (tea.Model, tea.Cmd) {
				m.leaveForm()
				cmd := confirmArchiveDeleteAgents(&m, m.listing.liveAgents(), agents)

				return m, cmd
			},
		},
	)
}

func workflowArchiveMenu(s panelShared, workflow panel.Workflow) submenu {
	return liveArchiveMenu(liveArchive{
		label: "archive with its agents",
		keys:  s.keys.ArchiveMenu.Archive,
		run: func(m model) (tea.Model, tea.Cmd) {
			return m.closeMenuWith(func(next model) (model, tea.Cmd) { return next.archiveWorkflow(workflow) })
		},
	})
}

func scheduleArchiveMenu(s panelShared, schedule panel.Schedule) submenu {
	return liveArchiveMenu(liveArchive{
		label: "archive",
		keys:  s.keys.ArchiveMenu.Archive,
		run: func(m model) (tea.Model, tea.Cmd) {
			return m.closeMenuWith(func(next model) (model, tea.Cmd) { return next.archiveSchedule(schedule) })
		},
	})
}

// confirmArchiveDeleteAgents asks before live agents are archived and then deleted for good.
func confirmArchiveDeleteAgents(h host, live []panel.Agent, agents []panel.Agent) tea.Cmd {
	form, confirmed := deleteAgentsForm(agents, unnamedChildren(live, agents), formKeys())

	return h.openForm(formState{
		kind:   formArchiveDelete,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m.archiveAndDeleteAgents(agents)
		}),
	})
}

// closeMenuWith takes the menu down and makes the write that was picked.
func (m model) closeMenuWith(write func(model) (model, tea.Cmd)) (tea.Model, tea.Cmd) {
	closed, back := m.closeForm()
	next, _ := closed.(model)
	written, cmd := write(next)

	return written, tea.Batch(back, cmd)
}
