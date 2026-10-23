package api

import (
	"fmt"

	"komrad/internal/ui/form"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

// Write is a write whose landing carries only whether it failed.
func (a Clients) Write(doing, summary string, write func(Clients) error) screen.Write {
	return a.AwaitingWrite(AwaitingWriteParams{
		Doing:   doing,
		Summary: summary,
		Write:   write,
	})
}

// AwaitingWriteParams is a write and the rows the panel waits on once it lands.
type AwaitingWriteParams struct {
	Doing   string
	Summary string
	Awaits  screen.Awaits
	Write   func(Clients) error
}

// AwaitingWrite is a write that is held, once it lands, until the panel shows what it did.
func (a Clients) AwaitingWrite(params AwaitingWriteParams) screen.Write {
	return screen.Write{
		Doing:   params.Doing,
		Summary: params.Summary,
		Send: func() screen.Landed {
			return screen.Landed{Err: params.Write(a), Awaits: params.Awaits}
		},
	}
}

// ArchivingWriteParams is a write that archives agents, their panes taken off the stage before it
// is sent.
type ArchivingWriteParams struct {
	Doing   string
	Summary string
	Agents  []panel.Agent
	Awaits  screen.Awaits
	Write   func(Clients) error
}

// ArchivingWrite sends the write the params name, taking the agents' panes off the stage first.
func (a Clients) ArchivingWrite(params ArchivingWriteParams) screen.Write {
	return screen.Write{
		Doing:   params.Doing,
		Summary: params.Summary,
		Unstage: params.Agents,
		Send: func() screen.Landed {
			err := params.Write(a)

			return screen.Landed{Err: err, Awaits: params.Awaits, Killed: err == nil}
		},
	}
}

// ArchiveTasksWrite archives the tasks along with the live agents on them.
func (a Clients) ArchiveTasksWrite(tasks []panel.Task, agents []panel.Agent) screen.Write {
	return a.ArchivingWrite(ArchivingWriteParams{
		Doing:   "archiving " + panel.DescribeTasks(tasks),
		Summary: "Could not archive " + panel.DescribeTasks(tasks),
		Agents:  agents,
		Write: func(clients Clients) error {
			for _, task := range tasks {
				if err := clients.ArchiveTask(task.ID); err != nil {
					return err
				}
			}

			return nil
		},
	})
}

// DeleteTasksWrite deletes the tasks' worktrees, and with records the tasks for good with
// everything on them.
func (a Clients) DeleteTasksWrite(tasks []panel.Task, agents []panel.Agent, records bool) screen.Write {
	return a.ArchivingWrite(ArchivingWriteParams{
		Doing:   "deleting " + panel.DescribeTasks(tasks),
		Summary: "Could not delete " + panel.DescribeTasks(tasks),
		Agents:  agents,
		Write: func(clients Clients) error {
			return clients.DeleteTasks(panel.TaskIDs(tasks), records)
		},
	})
}

// ConfirmDeleteTasks asks what goes before the tasks are deleted.
func (a Clients) ConfirmDeleteTasks(in screen.Inputs, tasks []panel.Task) screen.Request {
	warning := ""
	if agentCount := len(in.Listing.AgentsOnTasks(tasks)); agentCount > 0 {
		warning = fmt.Sprintf("Komrad archives %s first.", label.Count(agentCount, "live agent", "live agents"))
	}

	confirm := form.Delete{
		Title:           "Delete " + panel.DescribeTasks(tasks) + "?",
		Warning:         warning,
		Dirs:            panel.TaskDirs(tasks),
		StartsConfirmed: true,
		Values:          form.NewDeleteValues(in.Listing.HasAnyWorktreeToDelete(tasks)),
	}

	return confirm.Open(in, func(submitted form.Inputs) screen.Request {
		return a.DeleteTasksWrite(tasks, submitted.Listing.AgentsOnTasks(tasks), confirm.Values.DeletesRecords())
	})
}

// RenameSettled is a rename landed, for the screen that typed the name over its row.
type RenameSettled struct {
	Row string
	Err error
}

// RenameWriteParams is a name typed over a row and the write that sends it.
type RenameWriteParams struct {
	Doing   string
	Summary string
	Row     string
	Write   func(Clients) error
}

// RenameWrite sends a name typed over a row, handing the outcome to the screens that type names.
func (a Clients) RenameWrite(params RenameWriteParams) screen.Write {
	return screen.Write{
		Doing:   params.Doing,
		Summary: params.Summary,
		Send: func() screen.Landed {
			err := params.Write(a)

			return screen.Landed{Err: err, Then: RenameSettled{Row: params.Row, Err: err}}
		},
	}
}

// editedSaid is what the agent hears when the reader's editor on its worktree closes.
const editedSaid = "I had your worktree open in my editor and have closed it."

// EditedWrite tells an agent the reader's editor on its worktree has closed, naming the files the
// reader opened in it.
func (a Clients) EditedWrite(agent panel.Agent, touched []string) screen.Write {
	return a.Write("telling "+agent.Name+" about your edits", "Could not tell "+agent.Name+" about your edits", func(c Clients) error {
		return c.SendMail(agent.ID, editedSaid+touchedSaid(touched))
	})
}

// touchedSaid names the files the reader opened an editor on.
func touchedSaid(touched []string) string {
	if len(touched) == 0 {
		return ""
	}

	return fmt.Sprintf(" I opened these files in it, so read them before you trust what you "+
		"remember of them: %s.", joinList(touched))
}

// joinList is a list of paths as a sentence rather than as a slice.
func joinList(items []string) string {
	switch len(items) {
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	}

	said := ""
	for i, item := range items[:len(items)-1] {
		if i > 0 {
			said += ", "
		}

		said += item
	}

	return said + " and " + items[len(items)-1]
}
