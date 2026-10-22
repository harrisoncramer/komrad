package ui

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/record"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

	"charm.land/huh/v2"
)

// taskFields is a task's record, with what the record says of its writer where one is listed.
func taskFields(params checkoutFieldsParams, writer []record.Field) []record.Field {
	task := params.task

	directory := label.OrDash(task.Cwd)
	if task.IsWorktreeRemoved() {
		directory = "— (" + string(task.WorktreeRemovalReason) + ")"
	}
	fields := []record.Field{{Label: "directory", Value: directory}}
	fields = append(fields, checkoutFields(params)...)
	fields = append(fields, writer...)

	fields = append(fields, record.Field{Label: "task_id", Value: task.ID})
	if created := label.Friendly(task.CreatedAt); created != "" {
		fields = append(fields, record.Field{Label: "created", Value: created})
	}
	if task.ArchivedAt != nil {
		if archived := label.Friendly(*task.ArchivedAt); archived != "" {
			fields = append(fields, record.Field{Label: "archived", Value: archived})
		}
	}

	return fields
}

// taskMarkdown is what agents have written down about a task: its goal and its notes.
func taskMarkdown(task panel.Task) string {
	goal := task.Goal
	if strings.TrimSpace(goal) == "" {
		goal = "_no goal set_"
	}
	notes := task.Notes
	if strings.TrimSpace(notes) == "" {
		notes = "_no notes yet_"
	}

	return paint.DocumentMarkdown(task.Title, "", goal+"\n\n## Notes\n\n"+notes, "")
}

// isShowingNotes reports whether the row stands for the task whose notes are shown in place of its
// writer's pane: the task's own row, or its head in the triage zone.
func (a *agentsScreen) isShowingNotes(r agentRow) bool {
	switch {
	case a.notes == "":
		return false
	case r.isTask():
		return r.task.ID == a.notes
	}

	return r.triage && !r.header && !r.underTask && r.item.Agent.TaskID == a.notes
}

// notesEntry swaps the pane beside the list between the task's notes and its writer's pane.
func (a *agentsScreen) notesEntry(row agentRow) menu.Entry {
	if a.notes == row.task.ID {
		return menu.Leaving("close the notes", a.deps.keys.Menu.Notes, func(screen.Inputs) screen.Request { return a.closeNotes() })
	}

	return menu.Leaving("notes", a.deps.keys.Menu.Notes, func(screen.Inputs) screen.Request {
		a.notes = row.task.ID

		return screen.Restage{}
	})
}

// closeNotes puts the writer's pane back in place of the task's notes.
func (a *agentsScreen) closeNotes() screen.Request {
	a.notes = ""

	return screen.Restage{}
}

// isLiveWriter reports whether the agent is a writer still working on its task.
func isLiveWriter(agent panel.Agent) bool {
	return agent.ArchivedAt == nil && agent.Role == panel.RoleWriter &&
		agent.State != panel.StateFailedToCreate
}

// taskDirs is the directories of the tasks that have one still on disk.
func taskDirs(tasks []panel.Task) []string {
	return xslice.Map(xslice.Filter(tasks, func(task panel.Task) bool { return task.Cwd != "" && !task.IsWorktreeRemoved() }), func(task panel.Task) string { return task.Cwd })
}

// confirmDeleteTasks asks what goes before the tasks are deleted.
func confirmDeleteTasks(in screen.Inputs, clients api.Clients, tasks []panel.Task) screen.Request {
	confirm := deleteTasksConfirm(tasks, len(in.Listing.AgentsOnTasks(tasks)), in.Listing.HasAnyWorktreeToDelete(tasks))

	return confirm.open(in, func(submitted form.Inputs) screen.Request {
		return clients.DeleteTasksWrite(tasks, submitted.Listing.AgentsOnTasks(tasks), confirm.values.deletesRecords())
	})
}

// isEveryTaskHidden reports whether there are tasks and every one of them is hidden.
func isEveryTaskHidden(tasks []panel.Task) bool {
	return len(tasks) > 0 && !slices.ContainsFunc(tasks, func(task panel.Task) bool { return !task.Hidden })
}

// hideTasks hides the tasks, or unhides them when they are all hidden.
func (a *agentsScreen) hideTasks(tasks []panel.Task) screen.Request {
	hidden := !isEveryTaskHidden(tasks)
	changed := xslice.Filter(tasks, func(task panel.Task) bool { return task.Hidden != hidden })

	doing, failure := "hiding "+panel.DescribeTasks(tasks), "Could not hide "+panel.DescribeTasks(tasks)
	if !hidden {
		doing, failure = "unhiding "+panel.DescribeTasks(tasks), "Could not unhide "+panel.DescribeTasks(tasks)
	}

	return a.deps.clients.Write(doing, failure, func(clients api.Clients) error {
		for _, task := range changed {
			if err := clients.SetTaskHidden(task.ID, hidden); err != nil {
				return err
			}
		}

		return nil
	})
}

// maxTaskTitle is the longest title a task takes.
const maxTaskTitle = 200

type taskEditValues struct {
	Title string
	Goal  string
	Notes string
}

// openEditTask opens the form that edits a task's title, goal and notes.
func (a *agentsScreen) openEditTask(task panel.Task) screen.Request {
	values := &taskEditValues{
		Title: task.Title,
		Goal:  task.Goal,
		Notes: task.Notes,
	}

	return form.Open{Form: form.Form{
		Build: func(form.Inputs) (*huh.Form, []string) {
			return editTaskForm(fixedKeys(), values, task.Slug)
		},
		Editable: []form.Editable{{Key: "goal", Value: &values.Goal}, {Key: "notes", Value: &values.Notes}},
		Submit: func(form.Inputs) screen.Request {
			title, goal := strings.TrimSpace(values.Title), strings.TrimSpace(values.Goal)
			if err := cmp.Or(checkTaskTitle(title), checkTaskGoal(goal)); err != nil {
				return form.Refuse{Err: err}
			}

			params := api.UpdateTaskParams{
				ID:    task.ID,
				Title: &title,
				Goal:  &goal,
			}
			if notes := strings.TrimSpace(values.Notes); notes != strings.TrimSpace(task.Notes) {
				params.Notes, params.NotesUpdatedAt = &notes, task.NotesUpdatedAt
			}

			return a.deps.clients.Write(
				"saving "+task.Slug,
				fmt.Sprintf("Could not save task %q", task.Slug),
				func(clients api.Clients) error { return clients.UpdateTask(params) },
			)
		},
	}}
}

func editTaskForm(fixed config.FixedKeys, values *taskEditValues, slug string) (*huh.Form, []string) {
	return form.Of(fixed,
		huh.NewInput().Key("title").Title(fmt.Sprintf("Task %q", slug)).Description("its title").
			CharLimit(maxTaskTitle).Validate(checkTaskTitle).Value(&values.Title),
		form.Text("goal").Title("Goal").
			Description(fmt.Sprintf("the outcome that finishes this task; %s opens $EDITOR; %s submits", keys.Label(fixed.Editor), keys.Label(fixed.Submit))).
			Validate(checkTaskGoal).Value(&values.Goal),
		form.Text("notes").Title("Notes").
			Description(fmt.Sprintf("findings and decisions so far; %s opens $EDITOR; %s submits", keys.Label(fixed.Editor), keys.Label(fixed.Submit))).
			Value(&values.Notes),
	)
}

func checkTaskTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return errors.New("a task needs a title")
	}

	return nil
}

func checkTaskGoal(goal string) error {
	if strings.TrimSpace(goal) == "" {
		return errors.New("a task needs a goal")
	}

	return nil
}

// taskRowOf is the task row the hovered row acts as: itself on a task's header, and the header of
// the agent's task on a row of the triage zone.
func (a *agentsScreen) taskRowOf(in screen.Inputs, row agentRow) (agentRow, bool) {
	switch {
	case row.isTask():
		return row, true
	case !row.triage || row.header || row.item.Agent.TaskID == "":
		return agentRow{}, false
	}

	tree := treeRows(treeRowsParams{
		workflows:         []panel.Workflow{row.workflow},
		agents:            in.Listing.Agents,
		showHidden:        a.showHidden,
		foldOrchestrators: true,
		tasks:             in.Listing.Tasks,
	})
	at := slices.IndexFunc(tree, func(laid agentRow) bool {
		return laid.isTask() && laid.task.ID == row.item.Agent.TaskID
	})
	if at < 0 {
		return agentRow{}, false
	}

	return tree[at], true
}

// chosenWork is what a selection of agents acts on.
type chosenWork struct {
	chosen     []panel.Agent
	wholeTasks []panel.Task
	loneAgents []panel.Agent
}

// splitChosen sorts the chosen agents into the tasks whose writer is chosen and the agents left over.
func splitChosen(l panel.Listing, chosen []panel.Agent) chosenWork {
	writers := xslice.Filter(chosen, func(agent panel.Agent) bool { return agent.TaskID != "" && agent.Role == panel.RoleWriter })
	wholeTasks := xslice.Map(xslice.UniqueBy(writers, func(agent panel.Agent) string { return agent.TaskID }), l.Tasks.FindForAgent)

	whole := panel.TaskIDs(wholeTasks)
	loneAgents := xslice.Filter(chosen, func(agent panel.Agent) bool { return !slices.Contains(whole, agent.TaskID) })

	return chosenWork{
		chosen:     chosen,
		wholeTasks: wholeTasks,
		loneAgents: loneAgents,
	}
}

// deleteOffSaid is why the work cannot be deleted, empty when it can.
func (w chosenWork) deleteOffSaid() string {
	if len(w.loneAgents) == 0 {
		return ""
	}

	return "will only be deleted with its task or workflow"
}

// affected is every live agent the work takes.
func (w chosenWork) affected(l panel.Listing) []panel.Agent {
	return slices.Concat(l.AgentsOnTasks(w.wholeTasks), w.loneAgents)
}
