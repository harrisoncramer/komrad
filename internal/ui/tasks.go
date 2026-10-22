package ui

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/form"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/menu"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// taskFields is a task's record, with what the record says of its writer where one is listed.
func taskFields(params checkoutFieldsParams, writer []recordField) []recordField {
	task := params.task

	directory := label.OrDash(task.Cwd)
	if task.IsWorktreeRemoved() {
		directory = "— (" + string(task.WorktreeRemovalReason) + ")"
	}
	fields := []recordField{{label: "directory", value: directory}}
	fields = append(fields, checkoutFields(params)...)
	fields = append(fields, writer...)

	fields = append(fields, recordField{label: "task_id", value: task.ID})
	if created := label.Friendly(task.CreatedAt); created != "" {
		fields = append(fields, recordField{label: "created", value: created})
	}
	if task.ArchivedAt != nil {
		if archived := label.Friendly(*task.ArchivedAt); archived != "" {
			fields = append(fields, recordField{label: "archived", value: archived})
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

	return renderDocument(task.Title, "", goal+"\n\n## Notes\n\n"+notes, "")
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
func (a *agentsScreen) notesEntry(m *model, row agentRow) menu.Entry {
	if a.notes == row.task.ID {
		return menu.Leaving("close the notes", m.keys.Menu.Notes, func() tea.Cmd { return a.closeNotes(m) })
	}

	return menu.Leaving("notes", m.keys.Menu.Notes, func() tea.Cmd {
		a.notes = row.task.ID
		m.stageSelection()

		return nil
	})
}

// closeNotes puts the writer's pane back in place of the task's notes.
func (a *agentsScreen) closeNotes(m *model) tea.Cmd {
	a.notes = ""
	m.stageSelection()

	return nil
}

// isLiveWriter reports whether the agent is a writer still working on its task.
func isLiveWriter(agent panel.Agent) bool {
	return agent.ArchivedAt == nil && agent.Role == panel.RoleWriter &&
		agent.State != panel.StateFailedToCreate
}

func describeTasks(tasks []panel.Task) string {
	if len(tasks) == 1 {
		return fmt.Sprintf("task %q", tasks[0].Slug)
	}

	return fmt.Sprintf("%d tasks", len(tasks))
}

// taskDirs is the directories of the tasks that have one still on disk.
func taskDirs(tasks []panel.Task) []string {
	return xslice.Map(xslice.Filter(tasks, func(task panel.Task) bool { return task.Cwd != "" && !task.IsWorktreeRemoved() }), func(task panel.Task) string { return task.Cwd })
}

// archiveTasks archives the tasks along with the live agents on them.
func (m *model) archiveTasks(tasks []panel.Task) tea.Cmd {
	return m.apply(m.archivingWrite(archivingWriteParams{
		doing:   "archiving " + describeTasks(tasks),
		summary: "Could not archive " + describeTasks(tasks),
		agents:  m.listing.AgentsOnTasks(tasks),
		write: func(a api) error {
			for _, task := range tasks {
				if err := a.archiveTask(task.ID); err != nil {
					return err
				}
			}

			return nil
		},
	}))
}

// deleteTasks deletes the tasks' worktrees, and with records the tasks for good with everything on them.
func (m *model) deleteTasks(tasks []panel.Task, records bool) screen.Write {
	return m.archivingWrite(archivingWriteParams{
		doing:   "deleting " + describeTasks(tasks),
		summary: "Could not delete " + describeTasks(tasks),
		agents:  m.listing.AgentsOnTasks(tasks),
		write: func(a api) error {
			return a.deleteTasks(panel.TaskIDs(tasks), records)
		},
	})
}

// confirmDeleteTasks asks what goes before the tasks are deleted.
func confirmDeleteTasks(m *model, tasks []panel.Task) tea.Cmd {
	confirm := deleteTasksConfirm(tasks, len(m.listing.AgentsOnTasks(tasks)), m.listing.HasAnyWorktreeToDelete(tasks))

	return confirm.open(m, func() screen.Request {
		return m.deleteTasks(tasks, confirm.values.deletesRecords())
	})
}

// isEveryTaskHidden reports whether there are tasks and every one of them is hidden.
func isEveryTaskHidden(tasks []panel.Task) bool {
	return len(tasks) > 0 && !slices.ContainsFunc(tasks, func(task panel.Task) bool { return !task.Hidden })
}

// hideTasks hides the tasks, or unhides them when they are all hidden.
func hideTasks(m *model, tasks []panel.Task) tea.Cmd {
	hidden := !isEveryTaskHidden(tasks)
	changed := xslice.Filter(tasks, func(task panel.Task) bool { return task.Hidden != hidden })

	doing, failure := "hiding "+describeTasks(tasks), "Could not hide "+describeTasks(tasks)
	if !hidden {
		doing, failure = "unhiding "+describeTasks(tasks), "Could not unhide "+describeTasks(tasks)
	}

	return m.apply(m.write(doing, failure, func(a api) error {
		for _, task := range changed {
			if err := a.setTaskHidden(task.ID, hidden); err != nil {
				return err
			}
		}

		return nil
	}))
}

// maxTaskTitle is the longest title a task takes.
const maxTaskTitle = 200

type taskEditValues struct {
	Title string
	Goal  string
	Notes string
}

// openEditTask opens the form that edits a task's title, goal and notes.
func openEditTask(m *model, task panel.Task) tea.Cmd {
	values := &taskEditValues{
		Title: task.Title,
		Goal:  task.Goal,
		Notes: task.Notes,
	}

	return m.apply(form.Open{Form: form.Form{
		Build: func(form.Inputs) (*huh.Form, []string) {
			return editTaskForm(fixedKeys(), values, task.Slug)
		},
		Editable: []form.Editable{{Key: "goal", Value: &values.Goal}, {Key: "notes", Value: &values.Notes}},
		Submit: func(form.Inputs) screen.Request {
			title, goal := strings.TrimSpace(values.Title), strings.TrimSpace(values.Goal)
			if err := cmp.Or(checkTaskTitle(title), checkTaskGoal(goal)); err != nil {
				return form.Refuse{Err: err}
			}

			params := updateTaskParams{
				id:    task.ID,
				title: &title,
				goal:  &goal,
			}
			if notes := strings.TrimSpace(values.Notes); notes != strings.TrimSpace(task.Notes) {
				params.notes, params.notesUpdatedAt = &notes, task.NotesUpdatedAt
			}

			return m.write(
				"saving "+task.Slug,
				fmt.Sprintf("Could not save task %q", task.Slug),
				func(a api) error { return a.updateTask(params) },
			)
		},
	}})
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
func (a *agentsScreen) taskRowOf(m *model, row agentRow) (agentRow, bool) {
	switch {
	case row.isTask():
		return row, true
	case !row.triage || row.header || row.item.Agent.TaskID == "":
		return agentRow{}, false
	}

	tree := treeRows(treeRowsParams{
		workflows:         []panel.Workflow{row.workflow},
		agents:            m.listing.Agents,
		showHidden:        a.showHidden,
		foldOrchestrators: true,
		tasks:             m.listing.Tasks,
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
