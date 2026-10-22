package ui

import (
	"errors"
	"fmt"

	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

var (
	errNeverStarted    = errors.New("this agent never got as far as starting, so there is no conversation to open")
	errOpensOneAgent   = errors.New("restore and open works on one agent at a time")
	errWorktreeRemoved = errors.New("this task's worktree was removed, so there is no checkout to run in")
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
func (t archived) opensAgent(l panel.Listing) (*panel.Agent, error) {
	if t.resumed == nil {
		return nil, errOpensOneAgent
	}

	if !t.resumed.State.DoesPaneDecide() {
		return nil, errNeverStarted
	}

	if l.Tasks.FindForAgent(*t.resumed).IsWorktreeRemoved() {
		return nil, errWorktreeRemoved
	}

	return t.resumed, nil
}

// deleteOffSaid is why the target cannot be deleted, empty when it can.
func (t archived) deleteOffSaid() string {
	if t.schedule != nil || t.workflow != nil {
		return ""
	}

	return t.work.deleteOffSaid()
}

// archiveMenu is the archive's actions on a row, opened from its menu.
func archiveMenu(m *model, target archived) menu.Menu {
	restore, remove := "restore", "delete for good"
	if target.chosen {
		restore, remove = "restore all", "delete all for good"
	}

	_, err := target.opensAgent(m.listing)

	return menu.Menu{
		Title: "archive",
		Entries: []menu.Entry{
			menu.Holding(restore, m.keys.ArchiveMenu.Restore, func() screen.Request { return restoreArchived(m, target) }),
			{
				Label: "restore and open",
				Keys:  m.keys.ArchiveMenu.RestoreOpen,
				Off:   err != nil,
				After: menu.Hold,
				Pick:  func() screen.Request { return m.restoreAndOpen(target) },
			},
			menu.DrawnOff(menu.Leaving(remove, m.keys.ArchiveMenu.Delete, func() screen.Request {
				return confirmDeleteArchivedRow(m, target)
			}), target.deleteOffSaid()),
		},
	}
}

// deletedEntries are the deleted list's actions on a row, offering restore only on a workflow.
func deletedEntries(m *model, target archived) []menu.Entry {
	remove := "delete for good"
	if target.chosen {
		remove = "delete all for good"
	}

	var entries []menu.Entry
	if target.workflow != nil {
		entries = append(entries, menu.Holding("restore", m.keys.Menu.Restore, func() screen.Request { return restoreArchived(m, target) }))
	}

	return append(entries, menu.DrawnOff(menu.Leaving(remove, m.keys.Menu.Delete, func() screen.Request {
		return confirmDeleteArchivedRow(m, target)
	}), target.deleteOffSaid()))
}

// restoreAndOpen restores the work and puts one agent's conversation back in a pane, or says why it cannot.
func (m *model) restoreAndOpen(target archived) screen.Request {
	resumed, err := target.opensAgent(m.listing)
	if err != nil {
		return screen.Report{Summary: "Could not restore and open " + target.name(), Err: err}
	}

	return restoreAndResume(m, target.work.chosen, *resumed)
}

func restoreArchived(m *model, target archived) screen.Request {
	switch {
	case target.schedule != nil:
		schedule := *target.schedule

		return m.awaitingWrite(
			"restoring "+schedule.Name,
			fmt.Sprintf("Could not restore schedule %q", schedule.Name),
			screen.Awaits{Gone: screen.RowsIn(screen.ListArchivedSchedules, schedule.ID)},
			func(c api) error { return c.restoreSchedule(schedule.ID) },
		)
	case target.workflow != nil:
		workflow := *target.workflow

		return m.awaitingWrite(
			"restoring "+workflow.Name,
			fmt.Sprintf("Could not restore workflow %q", workflow.Name),
			screen.Awaits{Gone: screen.RowsIn(screen.ListArchivedWorkflows, workflow.ID)},
			func(c api) error { return c.restoreWorkflow(workflow.ID) },
		)
	}

	return restoreAgents(m, target.work.chosen)
}

func confirmDeleteArchivedRow(m *model, target archived) screen.Request {
	switch {
	case target.schedule != nil:
		return confirmDeleteSchedule(m, *target.schedule)
	case target.workflow != nil:
		return confirmDeleteWorkflow(m, *target.workflow)
	}

	return confirmDeleteChosenAgents(m, target.work)
}

// liveArchiveMenu is the archive menu on a row that is not archived yet.
func liveArchiveMenu(entries ...menu.Entry) menu.Menu {
	return menu.Menu{
		Title:   "archive",
		Entries: entries,
	}
}

// agentArchiveMenu archives the hovered agent alone, its task left standing.
func agentArchiveMenu(m *model, agent panel.Agent) menu.Menu {
	return liveArchiveMenu(menu.Entry{
		Label: "archive this agent",
		Keys:  m.keys.ArchiveMenu.Archive,
		After: menu.Hold,
		Pick:  func() screen.Request { return m.archiveAgents([]panel.Agent{agent}) },
	})
}

func workflowArchiveMenu(m *model, workflow panel.Workflow) menu.Menu {
	return liveArchiveMenu(menu.Entry{
		Label: "archive with its agents",
		Keys:  m.keys.ArchiveMenu.Archive,
		After: menu.Hold,
		Pick:  func() screen.Request { return m.archiveWorkflow(workflow) },
	})
}

// taskArchiveMenu archives the hovered task with its agents, or deletes it for good.
func taskArchiveMenu(m *model, task panel.Task) menu.Menu {
	tasks := []panel.Task{task}

	return liveArchiveMenu(
		menu.Entry{
			Label: "archive with its agents",
			Keys:  m.keys.ArchiveMenu.Archive,
			After: menu.Hold,
			Pick:  func() screen.Request { return m.archiveTasks(tasks) },
		},
		menu.Leaving("archive and delete for good", m.keys.ArchiveMenu.Delete, func() screen.Request {
			return confirmDeleteTasks(m, tasks)
		}),
	)
}

// chosenArchiveMenu archives the chosen agents, or deletes them for good with the task of each
// chosen writer.
func chosenArchiveMenu(m *model, work chosenWork) menu.Menu {
	return liveArchiveMenu(
		menu.Entry{
			Label: "archive all",
			Keys:  m.keys.ArchiveMenu.Archive,
			After: menu.Hold,
			Pick:  func() screen.Request { return m.archiveChosenAgents(work) },
		},
		menu.DrawnOff(menu.Leaving("archive and delete all for good", m.keys.ArchiveMenu.Delete, func() screen.Request {
			return confirmDeleteChosenAgents(m, work)
		}), work.deleteOffSaid()),
	)
}

// archiveChosenAgents archives the task of each chosen writer with its agents and the other chosen
// agents alone.
func (m *model) archiveChosenAgents(work chosenWork) screen.Request {
	subject := describeAgents(work.chosen)

	return m.archivingWrite(archivingWriteParams{
		doing:   "archiving " + subject,
		summary: "Could not archive " + subject,
		agents:  work.affected(m.listing),
		write: func(a api) error {
			for _, task := range work.wholeTasks {
				if err := a.archiveTask(task.ID); err != nil {
					return err
				}
			}
			if len(work.loneAgents) == 0 {
				return nil
			}

			return a.archiveAgents(panel.IDs(work.loneAgents))
		},
	})
}

// deleteChosenWrite is the write that deletes the task of each chosen writer, with its agents when
// records go too.
func (m *model) deleteChosenWrite(work chosenWork, records bool) screen.Write {
	subject := describeAgents(work.chosen)

	return m.archivingWrite(archivingWriteParams{
		doing:   "deleting " + subject,
		summary: "Could not delete " + subject,
		agents:  work.affected(m.listing),
		awaits:  awaitDeleted(records, screen.RowsIn(screen.ListArchivedAgents, panel.IDs(work.chosen)...)),
		write: func(a api) error {
			return a.deleteTasks(panel.TaskIDs(work.wholeTasks), records)
		},
	})
}

// confirmDeleteChosenAgents asks what goes before the chosen agents are deleted.
func confirmDeleteChosenAgents(m *model, work chosenWork) screen.Request {
	confirm := deleteChosenConfirm(work, m.listing.HasAnyWorktreeToDelete(work.wholeTasks))

	return confirm.open(m, func() screen.Request {
		return m.deleteChosenWrite(work, confirm.values.deletesRecords())
	})
}

func scheduleArchiveMenu(m *model, schedule panel.Schedule) menu.Menu {
	return liveArchiveMenu(menu.Entry{
		Label: "archive",
		Keys:  m.keys.ArchiveMenu.Archive,
		After: menu.Hold,
		Pick:  func() screen.Request { return m.archiveSchedule(schedule) },
	})
}
