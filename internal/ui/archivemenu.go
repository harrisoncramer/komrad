package ui

import (
	"errors"
	"fmt"

	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
)

var (
	errNeverStarted  = errors.New("this agent never got as far as starting, so there is no conversation to open")
	errOpensOneAgent = errors.New("restore and open puts one agent's conversation back in a pane, so it needs a single agent")
)

// archived is what the archive menu acts on: chosen or hovered work, a workflow, or a schedule.
type archived struct {
	work     chosenWork
	workflow *panel.Workflow
	schedule *panel.Schedule
	// resumed is the agent restore and open resumes, unset on a selection.
	resumed *panel.Agent
	title   string
	chosen  bool
}

// name is what the menu's band is titled with.
func (t archived) name() string {
	switch {
	case t.schedule != nil:
		return t.schedule.Name
	case t.workflow != nil:
		return t.workflow.Name
	}

	return t.title
}

// opensAgent is the one agent whose conversation can be put back in a pane, or why there is none.
func (t archived) opensAgent() (*panel.Agent, error) {
	if t.resumed == nil {
		return nil, errOpensOneAgent
	}

	if !t.resumed.State.DoesPaneDecide() {
		return nil, errNeverStarted
	}

	return t.resumed, nil
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

// restoreAndOpen restores the work and puts one agent's conversation back in a pane, or says why it cannot.
func (m *model) restoreAndOpen(target archived) tea.Cmd {
	resumed, err := target.opensAgent()
	if err != nil {
		m.report("Could not restore and open "+target.name(), err)

		return nil
	}

	return restoreAndResumeCmd(m, target.work.chosen, *resumed)
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

	return restoreAgentsCmd(m, target.work.chosen)
}

func confirmDeleteArchivedRow(m *model, target archived) tea.Cmd {
	switch {
	case target.schedule != nil:
		return confirmDeleteSchedule(m, *target.schedule)
	case target.workflow != nil:
		return confirmDeleteWorkflow(m, *target.workflow)
	}

	return confirmDeleteChosenAgents(m, target.work, archivedAgentsOf(m.listing))
}

// liveArchiveMenu is the archive menu on a row that is not archived yet.
func liveArchiveMenu(entries ...submenuEntry) submenu {
	return submenu{
		kind:    formArchiveMenu,
		title:   "archive",
		entries: entries,
	}
}

// agentArchiveMenu archives the hovered agent alone, its task left standing.
func agentArchiveMenu(m *model, agent panel.Agent) submenu {
	return liveArchiveMenu(submenuEntry{
		label: "archive this agent",
		keys:  m.keys.ArchiveMenu.Archive,
		write: func(m *model) tea.Cmd { return m.archiveAgents([]panel.Agent{agent}) },
	})
}

func workflowArchiveMenu(m *model, workflow panel.Workflow) submenu {
	return liveArchiveMenu(submenuEntry{
		label: "archive with its agents",
		keys:  m.keys.ArchiveMenu.Archive,
		write: func(m *model) tea.Cmd { return m.archiveWorkflow(workflow) },
	})
}

// taskArchiveMenu archives the hovered task with its agents, or deletes it for good.
func taskArchiveMenu(m *model, task panel.Task) submenu {
	tasks := []panel.Task{task}

	return liveArchiveMenu(
		submenuEntry{
			label: "archive with its agents",
			keys:  m.keys.ArchiveMenu.Archive,
			write: func(m *model) tea.Cmd { return m.archiveTasks(tasks) },
		},
		openEntry("archive and delete for good", m.keys.ArchiveMenu.Delete, func(m *model) tea.Cmd {
			return confirmDeleteTasks(m, tasks)
		}),
	)
}

// chosenArchiveMenu archives the chosen agents, or deletes them for good, taking the whole task of
// each chosen writer.
func chosenArchiveMenu(m *model, work chosenWork) submenu {
	return liveArchiveMenu(
		submenuEntry{
			label: "archive all",
			keys:  m.keys.ArchiveMenu.Archive,
			write: func(m *model) tea.Cmd { return m.archiveChosenAgents(work) },
		},
		openEntry("archive and delete all for good", m.keys.ArchiveMenu.Delete, func(m *model) tea.Cmd {
			return confirmDeleteChosenAgents(m, work, m.listing.liveAgents())
		}),
	)
}

// archiveChosenAgents archives the task of each chosen writer with its agents and the other chosen
// agents alone.
func (m *model) archiveChosenAgents(work chosenWork) tea.Cmd {
	pending := m.unstageAhead(work.affected(m.listing))
	subject := describeAgents(work.chosen)

	return pending.attach(m.writeCmd(
		"archiving "+subject,
		"Could not archive "+subject,
		func(a api) error {
			for _, task := range work.wholeTasks {
				if err := pending.noteArchive(a.archiveTask(task.ID)); err != nil {
					return err
				}
			}
			if len(work.loneAgents) == 0 {
				return nil
			}

			return pending.noteArchive(a.archiveAgents(panel.IDs(work.loneAgents)))
		},
	))
}

// deleteChosenAgents deletes the task of each chosen writer with its agents and the other chosen
// agents alone.
func (m *model) deleteChosenAgents(work chosenWork) tea.Cmd {
	pending := m.unstageAhead(work.affected(m.listing))
	subject := describeAgents(work.chosen)

	return pending.attach(m.awaitingWriteCmd(
		"deleting "+subject,
		"Could not delete "+subject,
		&awaits{gone: rowsIn(listArchivedAgents, panel.IDs(work.chosen)...)},
		func(a api) error {
			if len(work.wholeTasks) > 0 {
				if err := pending.noteArchive(a.deleteTasks(taskIDs(work.wholeTasks))); err != nil {
					return err
				}
			}
			if len(work.loneAgents) == 0 {
				return nil
			}

			live := xslice.Filter(work.loneAgents, func(agent panel.Agent) bool { return agent.ArchivedAt == nil })
			if len(live) > 0 {
				if err := pending.noteArchive(a.archiveAgents(panel.IDs(live))); err != nil {
					return err
				}
			}

			return a.deleteAgents(panel.IDs(work.loneAgents))
		},
	))
}

// confirmDeleteChosenAgents asks before the chosen agents are deleted for good.
func confirmDeleteChosenAgents(m *model, work chosenWork, pool []panel.Agent) tea.Cmd {
	form, confirmed := deleteChosenForm(work, unnamedReviewers(pool, work.chosen), fixedKeys())

	return m.openForm(formState{
		kind:   formArchiveDelete,
		form:   form,
		update: (*model).updateForm,
		submit: holdSubmit(func(m *model) tea.Cmd {
			if !*confirmed {
				return nil
			}

			return m.deleteChosenAgents(work)
		}),
	})
}

func scheduleArchiveMenu(m *model, schedule panel.Schedule) submenu {
	return liveArchiveMenu(submenuEntry{
		label: "archive",
		keys:  m.keys.ArchiveMenu.Archive,
		write: func(m *model) tea.Cmd { return m.archiveSchedule(schedule) },
	})
}
