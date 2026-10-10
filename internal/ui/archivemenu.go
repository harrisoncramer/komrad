package ui

import (
	"errors"
	"fmt"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
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

// archiveMenu is the archive's actions on a row, opened from its menu.
func archiveMenu(m *model, target archived) submenu {
	restore, remove := "restore", "delete for good"
	if target.chosen {
		restore, remove = "restore all", "delete all for good"
	}

	_, err := target.opensAgent()

	return submenu{
		kind:  formArchiveMenu,
		title: "archive",
		entries: []submenuEntry{
			writeEntry(restore, m.keys.ArchiveMenu.Restore, func(m *model) tea.Cmd { return restoreArchived(m, target) }),
			{
				label: "restore and open",
				keys:  m.keys.ArchiveMenu.RestoreOpen,
				isOff: err != nil,
				write: func(m *model) tea.Cmd { return m.restoreAndOpen(target) },
			},
			openEntry(remove, m.keys.ArchiveMenu.Delete, func(m *model) tea.Cmd {
				return confirmDeleteArchivedRow(m, target)
			}),
		},
	}
}

// restoreAndOpen restores the one agent and puts its conversation back in a pane, or says why it cannot.
func (m *model) restoreAndOpen(target archived) tea.Cmd {
	agent, err := target.opensAgent()
	if err != nil {
		m.report("Could not restore and open "+target.name(m.listing), err)

		return nil
	}

	return restoreAndResumeCmd(m, *agent)
}

func restoreArchived(m *model, target archived) tea.Cmd {
	switch {
	case target.schedule != nil:
		schedule := *target.schedule

		return m.awaitingWriteCmd(
			"restoring "+schedule.Name,
			fmt.Sprintf("Could not restore schedule %q", schedule.Name),
			&awaits{gone: rowsIn(listArchivedSchedules, schedule.ID)},
			func(c api) error { return c.restoreSchedule(schedule.ID) },
		)
	case target.workflow != nil:
		workflow := *target.workflow

		return m.awaitingWriteCmd(
			"restoring "+workflow.Name,
			fmt.Sprintf("Could not restore workflow %q", workflow.Name),
			&awaits{gone: rowsIn(listArchivedWorkflows, workflow.ID)},
			func(c api) error { return c.restoreWorkflow(workflow.ID) },
		)
	}

	return restoreAgentsCmd(m, target.agents)
}

func confirmDeleteArchivedRow(m *model, target archived) tea.Cmd {
	switch {
	case target.schedule != nil:
		return confirmDeleteSchedule(m, *target.schedule)
	case target.workflow != nil:
		return confirmDeleteWorkflow(m, *target.workflow)
	}

	return confirmDeleteArchived(m, target.agents)
}

// liveArchiveMenu is the archive menu on a row that is not archived yet.
func liveArchiveMenu(entries ...submenuEntry) submenu {
	return submenu{
		kind:    formArchiveMenu,
		title:   "archive",
		entries: entries,
	}
}

func agentsArchiveMenu(m *model, agents []panel.Agent, chosen bool) submenu {
	archive, remove := "archive", "archive and delete for good"
	if chosen {
		archive, remove = "archive all", "archive and delete all for good"
	}

	return liveArchiveMenu(
		submenuEntry{
			label: archive,
			keys:  m.keys.ArchiveMenu.Archive,
			write: func(m *model) tea.Cmd { return m.archiveAgents(agents) },
		},
		openEntry(remove, m.keys.ArchiveMenu.Delete, func(m *model) tea.Cmd {
			return confirmArchiveDeleteAgents(m, agents)
		}),
	)
}

func workflowArchiveMenu(m *model, workflow panel.Workflow) submenu {
	return liveArchiveMenu(submenuEntry{
		label: "archive with its agents",
		keys:  m.keys.ArchiveMenu.Archive,
		write: func(m *model) tea.Cmd { return m.archiveWorkflow(workflow) },
	})
}

func taskArchiveMenu(m *model, task panel.Task) submenu {
	return liveArchiveMenu(submenuEntry{
		label: "archive with its agents",
		keys:  m.keys.ArchiveMenu.Archive,
		write: func(m *model) tea.Cmd { return m.archiveTask(task) },
	})
}

func scheduleArchiveMenu(m *model, schedule panel.Schedule) submenu {
	return liveArchiveMenu(submenuEntry{
		label: "archive",
		keys:  m.keys.ArchiveMenu.Archive,
		write: func(m *model) tea.Cmd { return m.archiveSchedule(schedule) },
	})
}

// confirmArchiveDeleteAgents asks before live agents are archived and then deleted for good.
func confirmArchiveDeleteAgents(m *model, agents []panel.Agent) tea.Cmd {
	form, confirmed := deleteAgentsForm(agents, unnamedChildren(m.listing.liveAgents(), agents), fixedKeys())

	return m.openForm(formState{
		kind:   formArchiveDelete,
		form:   form,
		update: (*model).updateForm,
		submit: holdSubmit(func(m *model) tea.Cmd {
			if !*confirmed {
				return nil
			}

			return m.archiveAndDeleteAgents(agents)
		}),
	})
}
