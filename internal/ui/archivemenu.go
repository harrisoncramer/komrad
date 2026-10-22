package ui

import (
	"errors"
	"fmt"

	"komrad/config"
	"komrad/internal/ui/api"
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
func archiveMenu(m *model, bindings config.Keybindings, clients api.Clients, target archived) menu.Menu {
	restore, remove := "restore", "delete for good"
	if target.chosen {
		restore, remove = "restore all", "delete all for good"
	}

	_, err := target.opensAgent(m.listing)

	return menu.Menu{
		Title: "archive",
		Entries: []menu.Entry{
			menu.Holding(restore, bindings.ArchiveMenu.Restore, func() screen.Request { return restoreArchived(clients, target) }),
			{
				Label: "restore and open",
				Keys:  bindings.ArchiveMenu.RestoreOpen,
				Off:   err != nil,
				After: menu.Hold,
				Pick:  func() screen.Request { return m.restoreAndOpen(clients, target) },
			},
			menu.DrawnOff(menu.Leaving(remove, bindings.ArchiveMenu.Delete, func() screen.Request {
				return confirmDeleteArchivedRow(m, clients, target)
			}), target.deleteOffSaid()),
		},
	}
}

// deletedEntries are the deleted list's actions on a row, offering restore only on a workflow.
func deletedEntries(m *model, bindings config.Keybindings, clients api.Clients, target archived) []menu.Entry {
	remove := "delete for good"
	if target.chosen {
		remove = "delete all for good"
	}

	var entries []menu.Entry
	if target.workflow != nil {
		entries = append(entries, menu.Holding("restore", bindings.Menu.Restore, func() screen.Request { return restoreArchived(clients, target) }))
	}

	return append(entries, menu.DrawnOff(menu.Leaving(remove, bindings.Menu.Delete, func() screen.Request {
		return confirmDeleteArchivedRow(m, clients, target)
	}), target.deleteOffSaid()))
}

// restoreAndOpen restores the work and puts one agent's conversation back in a pane, or says why it cannot.
func (m *model) restoreAndOpen(clients api.Clients, target archived) screen.Request {
	resumed, err := target.opensAgent(m.listing)
	if err != nil {
		return screen.Report{Summary: "Could not restore and open " + target.name(), Err: err}
	}

	return restoreAndResume(clients, target.work.chosen, *resumed)
}

func restoreArchived(clients api.Clients, target archived) screen.Request {
	switch {
	case target.schedule != nil:
		schedule := *target.schedule

		return clients.AwaitingWrite(api.AwaitingWriteParams{
			Doing:   "restoring " + schedule.Name,
			Summary: fmt.Sprintf("Could not restore schedule %q", schedule.Name),
			Awaits:  screen.Awaits{Gone: screen.RowsIn(screen.ListArchivedSchedules, schedule.ID)},
			Write:   func(c api.Clients) error { return c.RestoreSchedule(schedule.ID) },
		})
	case target.workflow != nil:
		workflow := *target.workflow

		return clients.AwaitingWrite(api.AwaitingWriteParams{
			Doing:   "restoring " + workflow.Name,
			Summary: fmt.Sprintf("Could not restore workflow %q", workflow.Name),
			Awaits:  screen.Awaits{Gone: screen.RowsIn(screen.ListArchivedWorkflows, workflow.ID)},
			Write:   func(c api.Clients) error { return c.RestoreWorkflow(workflow.ID) },
		})
	}

	return restoreAgents(clients, target.work.chosen)
}

func confirmDeleteArchivedRow(m *model, clients api.Clients, target archived) screen.Request {
	switch {
	case target.schedule != nil:
		return confirmDeleteSchedule(clients, *target.schedule)
	case target.workflow != nil:
		return confirmDeleteWorkflow(m, clients, *target.workflow)
	}

	return confirmDeleteChosenAgents(m, clients, target.work)
}

// liveArchiveMenu is the archive menu on a row that is not archived yet.
func liveArchiveMenu(entries ...menu.Entry) menu.Menu {
	return menu.Menu{
		Title:   "archive",
		Entries: entries,
	}
}

// agentArchiveMenu archives the hovered agent alone, its task left standing.
func (a *agentsScreen) agentArchiveMenu(agent panel.Agent) menu.Menu {
	return liveArchiveMenu(menu.Entry{
		Label: "archive this agent",
		Keys:  a.deps.keys.ArchiveMenu.Archive,
		After: menu.Hold,
		Pick:  func() screen.Request { return a.archiveAgents([]panel.Agent{agent}) },
	})
}

func (a *agentsScreen) workflowArchiveMenu(m *model, workflow panel.Workflow) menu.Menu {
	return liveArchiveMenu(menu.Entry{
		Label: "archive with its agents",
		Keys:  a.deps.keys.ArchiveMenu.Archive,
		After: menu.Hold,
		Pick:  func() screen.Request { return a.archiveWorkflow(m, workflow) },
	})
}

// taskArchiveMenu archives the hovered task with its agents, or deletes it for good.
func (a *agentsScreen) taskArchiveMenu(m *model, task panel.Task) menu.Menu {
	tasks := []panel.Task{task}

	return liveArchiveMenu(
		menu.Entry{
			Label: "archive with its agents",
			Keys:  a.deps.keys.ArchiveMenu.Archive,
			After: menu.Hold,
			Pick:  func() screen.Request { return a.deps.clients.ArchiveTasksWrite(tasks, m.listing.AgentsOnTasks(tasks)) },
		},
		menu.Leaving("archive and delete for good", a.deps.keys.ArchiveMenu.Delete, func() screen.Request {
			return confirmDeleteTasks(m, a.deps.clients, tasks)
		}),
	)
}

// chosenArchiveMenu archives the chosen agents, or deletes them for good with the task of each
// chosen writer.
func (a *agentsScreen) chosenArchiveMenu(m *model, work chosenWork) menu.Menu {
	return liveArchiveMenu(
		menu.Entry{
			Label: "archive all",
			Keys:  a.deps.keys.ArchiveMenu.Archive,
			After: menu.Hold,
			Pick:  func() screen.Request { return a.archiveChosenAgents(m, work) },
		},
		menu.DrawnOff(menu.Leaving("archive and delete all for good", a.deps.keys.ArchiveMenu.Delete, func() screen.Request {
			return confirmDeleteChosenAgents(m, a.deps.clients, work)
		}), work.deleteOffSaid()),
	)
}

// archiveChosenAgents archives the task of each chosen writer with its agents and the other chosen
// agents alone.
func (a *agentsScreen) archiveChosenAgents(m *model, work chosenWork) screen.Request {
	subject := describeAgents(work.chosen)

	return a.deps.clients.ArchivingWrite(api.ArchivingWriteParams{
		Doing:   "archiving " + subject,
		Summary: "Could not archive " + subject,
		Agents:  work.affected(m.listing),
		Write: func(clients api.Clients) error {
			for _, task := range work.wholeTasks {
				if err := clients.ArchiveTask(task.ID); err != nil {
					return err
				}
			}
			if len(work.loneAgents) == 0 {
				return nil
			}

			return clients.ArchiveAgents(panel.IDs(work.loneAgents))
		},
	})
}

// deleteChosenWrite is the write that deletes the task of each chosen writer, with its agents when
// records go too.
func (m *model) deleteChosenWrite(clients api.Clients, work chosenWork, records bool) screen.Write {
	subject := describeAgents(work.chosen)

	return clients.ArchivingWrite(api.ArchivingWriteParams{
		Doing:   "deleting " + subject,
		Summary: "Could not delete " + subject,
		Agents:  work.affected(m.listing),
		Awaits:  awaitDeleted(records, screen.RowsIn(screen.ListArchivedAgents, panel.IDs(work.chosen)...)),
		Write: func(a api.Clients) error {
			return a.DeleteTasks(panel.TaskIDs(work.wholeTasks), records)
		},
	})
}

// confirmDeleteChosenAgents asks what goes before the chosen agents are deleted.
func confirmDeleteChosenAgents(m *model, clients api.Clients, work chosenWork) screen.Request {
	confirm := deleteChosenConfirm(work, m.listing.HasAnyWorktreeToDelete(work.wholeTasks))

	return confirm.open(m, func() screen.Request {
		return m.deleteChosenWrite(clients, work, confirm.values.deletesRecords())
	})
}

func (w *schedulesScreen) scheduleArchiveMenu(schedule panel.Schedule) menu.Menu {
	return liveArchiveMenu(menu.Entry{
		Label: "archive",
		Keys:  w.deps.keys.ArchiveMenu.Archive,
		After: menu.Hold,
		Pick:  func() screen.Request { return w.archiveSchedule(schedule) },
	})
}
