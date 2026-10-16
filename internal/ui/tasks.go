package ui

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"

	"komrad/config"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// taskIndex is the panel's tasks keyed by id.
type taskIndex map[string]panel.Task

// taskAgents is the agents on a task, the archived ones for an archived task and the live ones otherwise.
func (l listing) taskAgents(task panel.Task) []panel.AgentRow {
	pool := l.agents
	if task.ArchivedAt != nil {
		pool = l.archivedAgents
	}

	return xslice.Filter(pool, func(item panel.AgentRow) bool { return item.Agent.TaskID == task.ID })
}

// taskFields is a task's record, with what the record says of its writer where one is listed.
func taskFields(params checkoutFieldsParams, writer []recordField) []recordField {
	task := params.task

	directory := label.OrDash(task.Cwd)
	if task.WorktreeRemoved {
		directory = "— (removed)"
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
func (a *agentsScreen) notesEntry(m *model, row agentRow) submenuEntry {
	if a.notes == row.task.ID {
		return openEntry("close the notes", m.keys.Menu.Notes, a.closeNotes)
	}

	return openEntry("notes", m.keys.Menu.Notes, func(m *model) tea.Cmd {
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

// agentsOnTasks is the live agents on any of the tasks, hidden ones included.
func (l listing) agentsOnTasks(tasks []panel.Task) []panel.Agent {
	ids := taskIDs(tasks)
	onTasks := xslice.Filter(l.agents, func(item panel.AgentRow) bool { return slices.Contains(ids, item.Agent.TaskID) })

	return xslice.Map(onTasks, func(item panel.AgentRow) panel.Agent { return item.Agent })
}

// findForAgent is the agent's task, or one spelled from the agent when the index holds none.
func (t taskIndex) findForAgent(agent panel.Agent) panel.Task {
	if task, ok := t[agent.TaskID]; ok {
		return task
	}

	return panel.Task{
		ID:         agent.TaskID,
		WorkflowID: agent.WorkflowID,
	}
}

// isHidden reports whether the agent works on a hidden task.
func (t taskIndex) isHidden(agent panel.Agent) bool {
	return t[agent.TaskID].Hidden
}

// findTaskWriter is the task's writer, when the listing holds it.
func (l listing) findTaskWriter(task panel.Task) (panel.AgentRow, bool) {
	onTask := l.taskAgents(task)
	at := slices.IndexFunc(onTask, func(item panel.AgentRow) bool { return item.Agent.Role == panel.RoleWriter })
	if at < 0 {
		return panel.AgentRow{}, false
	}

	return onTask[at], true
}

// findWriterInCheckout is the task's writer while it works in the task's checkout.
func (l listing) findWriterInCheckout(task panel.Task) (panel.AgentRow, bool) {
	writer, ok := l.findTaskWriter(task)
	if !ok || task.Cwd == "" || writer.Agent.Cwd != task.Cwd {
		return panel.AgentRow{}, false
	}

	return writer, true
}

// isCheckoutOwner reports whether the agent is the one its task's checkout was provisioned for.
func isCheckoutOwner(agent panel.Agent) bool {
	return agent.Role == panel.RoleWriter && !agent.ProvisionDisabled
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

func taskIDs(tasks []panel.Task) []string {
	return xslice.Map(tasks, func(task panel.Task) string { return task.ID })
}

// taskDirs is the directories of the tasks that have one.
func taskDirs(tasks []panel.Task) []string {
	return xslice.Map(xslice.Filter(tasks, func(task panel.Task) bool { return task.Cwd != "" }), func(task panel.Task) string { return task.Cwd })
}

// archiveTasks archives the tasks along with the live agents on them.
func (m *model) archiveTasks(tasks []panel.Task) tea.Cmd {
	pending := m.unstageAhead(m.listing.agentsOnTasks(tasks))

	return pending.attach(m.writeCmd(
		"archiving "+describeTasks(tasks),
		"Could not archive "+describeTasks(tasks),
		func(a api) error {
			for _, task := range tasks {
				if err := pending.noteArchive(a.archiveTask(task.ID)); err != nil {
					return err
				}
			}

			return nil
		},
	))
}

// deleteTasks deletes the tasks for good, with their agents, documents, reviews and worktrees.
func (m *model) deleteTasks(tasks []panel.Task) tea.Cmd {
	pending := m.unstageAhead(m.listing.agentsOnTasks(tasks))

	return pending.attach(m.writeCmd(
		"deleting "+describeTasks(tasks),
		"Could not delete "+describeTasks(tasks),
		func(a api) error {
			return pending.noteArchive(a.deleteTasks(taskIDs(tasks)))
		},
	))
}

// confirmDeleteTasks asks before the tasks are deleted for good.
func confirmDeleteTasks(m *model, tasks []panel.Task) tea.Cmd {
	return deleteTasksConfirm(tasks, len(m.listing.agentsOnTasks(tasks))).open(m, func(m *model) tea.Cmd {
		return m.deleteTasks(tasks)
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

	return m.writeCmd(doing, failure, func(a api) error {
		for _, task := range changed {
			if err := a.setTaskHidden(task.ID, hidden); err != nil {
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
func openEditTask(m *model, task panel.Task) tea.Cmd {
	values := &taskEditValues{
		Title: task.Title,
		Goal:  task.Goal,
		Notes: task.Notes,
	}

	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind: formTask,
		Build: func(m *model) (*huh.Form, []string) {
			return editTaskForm(fixedKeys(), values, task.Slug)
		},
		Editable: []editableText{{key: "goal", value: &values.Goal}, {key: "notes", value: &values.Notes}},
		Submit: func(m *model) tea.Cmd {
			title, goal := strings.TrimSpace(values.Title), strings.TrimSpace(values.Goal)
			if err := cmp.Or(checkTaskTitle(title), checkTaskGoal(goal)); err != nil {
				m.form.refusal = err.Error()

				return formResized()
			}

			params := updateTaskParams{
				id:    task.ID,
				title: &title,
				goal:  &goal,
			}
			if notes := strings.TrimSpace(values.Notes); notes != strings.TrimSpace(task.Notes) {
				params.notes, params.notesUpdatedAt = &notes, task.NotesUpdatedAt
			}

			return holdSubmit(func(m *model) tea.Cmd {
				return m.writeCmd(
					"saving "+task.Slug,
					fmt.Sprintf("Could not save task %q", task.Slug),
					func(a api) error { return a.updateTask(params) },
				)
			})(m)
		},
	}))
}

func editTaskForm(keys config.FixedKeys, values *taskEditValues, slug string) (*huh.Form, []string) {
	return formOf(keys,
		huh.NewInput().Key("title").Title(fmt.Sprintf("Task %q", slug)).Description("its title").
			CharLimit(maxTaskTitle).Validate(checkTaskTitle).Value(&values.Title),
		textField("goal").Title("Goal").
			Description(fmt.Sprintf("the outcome that finishes this task; %s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).
			Validate(checkTaskGoal).Value(&values.Goal),
		textField("notes").Title("Notes").
			Description(fmt.Sprintf("findings and decisions so far; %s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).
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
		agents:            m.listing.agents,
		showHidden:        a.showHidden,
		foldOrchestrators: true,
		tasks:             m.listing.tasks,
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
func splitChosen(l listing, chosen []panel.Agent) chosenWork {
	writers := xslice.Filter(chosen, func(agent panel.Agent) bool { return agent.TaskID != "" && agent.Role == panel.RoleWriter })
	wholeTasks := xslice.Map(xslice.UniqueBy(writers, func(agent panel.Agent) string { return agent.TaskID }), l.tasks.findForAgent)

	whole := taskIDs(wholeTasks)
	loneAgents := xslice.Filter(chosen, func(agent panel.Agent) bool { return !slices.Contains(whole, agent.TaskID) })

	return chosenWork{
		chosen:     chosen,
		wholeTasks: wholeTasks,
		loneAgents: loneAgents,
	}
}

// affected is every live agent the work takes.
func (w chosenWork) affected(l listing) []panel.Agent {
	return slices.Concat(l.agentsOnTasks(w.wholeTasks), w.loneAgents)
}
