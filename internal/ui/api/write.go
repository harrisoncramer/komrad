package api

import (
	"fmt"
	"slices"

	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
)

// Write is a write whose landing carries only whether it failed.
func (a Clients) Write(doing, summary string, write func(Clients) error) screen.Write {
	return a.AwaitingWrite(doing, summary, screen.Awaits{}, write)
}

// AwaitingWrite is a write that is held, once it lands, until the panel shows what it did.
func (a Clients) AwaitingWrite(doing, summary string, awaits screen.Awaits, write func(Clients) error) screen.Write {
	return screen.Write{
		Doing:   doing,
		Summary: summary,
		Send: func() screen.Landed {
			return screen.Landed{Err: write(a), Awaits: awaits}
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

// RenameSettled is a rename landed, for the screen that typed the name over its row.
type RenameSettled struct {
	Row string
	Err error
}

// RenameWrite sends a name typed over a row, handing the outcome to the screens that type names.
func (a Clients) RenameWrite(doing, summary, row string, write func(Clients) error) screen.Write {
	return screen.Write{
		Doing:   doing,
		Summary: summary,
		Send: func() screen.Landed {
			err := write(a)

			return screen.Landed{Err: err, Then: RenameSettled{Row: row, Err: err}}
		},
	}
}

// ReleaseWrite lets an agent go, naming the files a reader had open over it.
func (a Clients) ReleaseWrite(summary string, agent panel.Agent, said string, edited []string) screen.Write {
	let := screen.Released{
		ID:    agent.ID,
		Name:  agent.Name,
		Files: slices.Clone(edited),
	}

	return screen.Write{
		Doing:   "letting " + agent.Name + " go",
		Summary: summary,
		Send: func() screen.Landed {
			return screen.Landed{
				Err:      a.Release(agent, said+touchedSaid(let.Files)),
				Released: &let,
			}
		},
	}
}

// touchedSaid names the files the reader opened an editor on.
func touchedSaid(touched []string) string {
	if len(touched) == 0 {
		return ""
	}

	return fmt.Sprintf(" I had these files open in an editor while you were paused, so read them "+
		"before you trust what you remember of them: %s.", joinList(touched))
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
