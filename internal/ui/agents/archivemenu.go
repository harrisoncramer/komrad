package ui

import (
	"errors"
	"fmt"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

var (
	errNeverStarted    = errors.New("this agent never got as far as starting, so there is no conversation to open")
	errOpensOneAgent   = errors.New("restore and open works on one agent at a time")
	errWorktreeRemoved = errors.New("this task's worktree was removed, so there is no checkout to run in")
)

// archived is what the archive menu acts on: chosen or hovered work, or a workflow.
type archived struct {
	work     chosenWork
	workflow *panel.Workflow
	// resumed is the agent restore and open resumes, unset on a selection.
	resumed *panel.Agent
	title   string
	chosen  bool
}

// name is what the menu's band is titled with.
func (t archived) name() string {
	if t.workflow != nil {
		return t.workflow.Name
	}

	return t.title
}

// opensAgent is the one agent whose conversation can be put back in a pane, or why there is none.
func (t archived) opensAgent(l panel.Listing) (*panel.Agent, error) {
	if t.resumed == nil {
		return nil, errOpensOneAgent
	}

	if !panel.DoesPaneDecide(t.resumed.State) {
		return nil, errNeverStarted
	}

	if l.Tasks.FindForAgent(*t.resumed).IsWorktreeRemoved() {
		return nil, errWorktreeRemoved
	}

	return t.resumed, nil
}

// deleteOffSaid is why the target cannot be deleted, empty when it can.
func (t archived) deleteOffSaid() string {
	if t.workflow != nil {
		return ""
	}

	return t.work.deleteOffSaid()
}

// archiveMenu is the archive's actions on a row, opened from its menu.
func archiveMenu(in screen.Inputs, bindings config.Keybindings, clients api.Clients, target archived) menu.Menu {
	restore, remove := "restore", "delete for good"
	if target.chosen {
		restore, remove = "restore all", "delete all for good"
	}

	_, err := target.opensAgent(in.Listing)

	return menu.Menu{
		Title: "archive",
		Entries: []menu.Entry{
			menu.Holding(restore, bindings.ArchiveMenu.Restore, func(screen.Inputs) screen.Request { return restoreArchived(clients, target) }),
			{
				Label: "restore and open",
				Keys:  bindings.ArchiveMenu.RestoreOpen,
				Off:   err != nil,
				After: menu.Hold,
				Pick:  func(in screen.Inputs) screen.Request { return restoreAndOpen(in.Listing, clients, target) },
			},
			menu.DrawnOff(menu.Leaving(remove, bindings.ArchiveMenu.Delete, func(in screen.Inputs) screen.Request {
				return confirmDeleteArchivedRow(in, clients, target)
			}), target.deleteOffSaid()),
		},
	}
}

// deletedEntries are the deleted list's actions on a row, offering restore only on a workflow.
func deletedEntries(bindings config.Keybindings, clients api.Clients, target archived) []menu.Entry {
	remove := "delete for good"
	if target.chosen {
		remove = "delete all for good"
	}

	var entries []menu.Entry
	if target.workflow != nil {
		entries = append(entries, menu.Holding("restore", bindings.Menu.Restore, func(screen.Inputs) screen.Request { return restoreArchived(clients, target) }))
	}

	return append(entries, menu.DrawnOff(menu.Leaving(remove, bindings.Menu.Delete, func(in screen.Inputs) screen.Request {
		return confirmDeleteArchivedRow(in, clients, target)
	}), target.deleteOffSaid()))
}

// restoreAndOpen restores the work and puts one agent's conversation back in a pane, or says why it cannot.
func restoreAndOpen(l panel.Listing, clients api.Clients, target archived) screen.Request {
	resumed, err := target.opensAgent(l)
	if err != nil {
		return screen.Report{Summary: "Could not restore and open " + target.name(), Err: err}
	}

	return restoreAndResume(clients, target.work.chosen, *resumed)
}

func restoreArchived(clients api.Clients, target archived) screen.Request {
	if target.workflow != nil {
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

func confirmDeleteArchivedRow(in screen.Inputs, clients api.Clients, target archived) screen.Request {
	if target.workflow != nil {
		return confirmDeleteWorkflow(in, clients, *target.workflow)
	}

	return confirmDeleteChosenAgents(in, clients, target.work)
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
		Pick:  func(screen.Inputs) screen.Request { return a.archiveAgents([]panel.Agent{agent}) },
	})
}

func (a *agentsScreen) workflowArchiveMenu(workflow panel.Workflow) menu.Menu {
	return liveArchiveMenu(menu.Entry{
		Label: "archive with its agents",
		Keys:  a.deps.keys.ArchiveMenu.Archive,
		After: menu.Hold,
		Pick:  func(in screen.Inputs) screen.Request { return a.archiveWorkflow(in, workflow) },
	})
}

// taskArchiveMenu archives the hovered task with its agents, or deletes it for good.
func (a *agentsScreen) taskArchiveMenu(task panel.Task) menu.Menu {
	tasks := []panel.Task{task}

	return liveArchiveMenu(
		menu.Entry{
			Label: "archive with its agents",
			Keys:  a.deps.keys.ArchiveMenu.Archive,
			After: menu.Hold,
			Pick: func(in screen.Inputs) screen.Request {
				return a.deps.clients.ArchiveTasksWrite(tasks, in.Listing.AgentsOnTasks(tasks))
			},
		},
		menu.Leaving("archive and delete for good", a.deps.keys.ArchiveMenu.Delete, func(in screen.Inputs) screen.Request {
			return a.deps.clients.ConfirmDeleteTasks(in, tasks)
		}),
	)
}

// chosenArchiveMenu archives the chosen agents, or deletes them for good with the task of each
// chosen writer.
func (a *agentsScreen) chosenArchiveMenu(work chosenWork) menu.Menu {
	return liveArchiveMenu(
		menu.Entry{
			Label: "archive all",
			Keys:  a.deps.keys.ArchiveMenu.Archive,
			After: menu.Hold,
			Pick:  func(in screen.Inputs) screen.Request { return a.archiveChosenAgents(in, work) },
		},
		menu.DrawnOff(menu.Leaving("archive and delete all for good", a.deps.keys.ArchiveMenu.Delete, func(in screen.Inputs) screen.Request {
			return confirmDeleteChosenAgents(in, a.deps.clients, work)
		}), work.deleteOffSaid()),
	)
}

// archiveChosenAgents archives the task of each chosen writer with its agents and the other chosen
// agents alone.
func (a *agentsScreen) archiveChosenAgents(in screen.Inputs, work chosenWork) screen.Request {
	subject := describeAgents(work.chosen)

	return a.deps.clients.ArchivingWrite(api.ArchivingWriteParams{
		Doing:   "archiving " + subject,
		Summary: "Could not archive " + subject,
		Agents:  work.affected(in.Listing),
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
func deleteChosenWrite(l panel.Listing, clients api.Clients, work chosenWork, records bool) screen.Write {
	subject := describeAgents(work.chosen)

	return clients.ArchivingWrite(api.ArchivingWriteParams{
		Doing:   "deleting " + subject,
		Summary: "Could not delete " + subject,
		Agents:  work.affected(l),
		Awaits:  awaitDeleted(records, screen.RowsIn(screen.ListArchivedAgents, panel.IDs(work.chosen)...)),
		Write: func(a api.Clients) error {
			return a.DeleteTasks(panel.TaskIDs(work.wholeTasks), records)
		},
	})
}

// confirmDeleteChosenAgents asks what goes before the chosen agents are deleted.
func confirmDeleteChosenAgents(in screen.Inputs, clients api.Clients, work chosenWork) screen.Request {
	confirm := deleteChosenConfirm(work, in.Listing.HasAnyWorktreeToDelete(work.wholeTasks))

	return confirm.Open(in, func(submitted form.Inputs) screen.Request {
		return deleteChosenWrite(submitted.Listing, clients, work, confirm.Values.DeletesRecords())
	})
}
