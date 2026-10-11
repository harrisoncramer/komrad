package ui

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"komrad/config"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

// taskAgents is the live agents on a task, oldest first.
func (l listing) taskAgents(taskID string) []panel.AgentRow {
	return xslice.Filter(l.agents, func(item panel.AgentRow) bool { return item.Agent.TaskID == taskID })
}

// taskFieldsParams is a task with the agents, writer and worktree read its info panel shows.
type taskFieldsParams struct {
	task     panel.Task
	agents   []panel.AgentRow
	writer   panel.AgentRow
	worktree worktreeInspection
	read     bool
	now      time.Time
}

func taskFields(params taskFieldsParams) []recordField {
	task, writer := params.task, params.writer
	names := xslice.Map(params.agents, func(item panel.AgentRow) string { return item.Agent.Name })
	fields := []recordField{
		{label: "task", value: task.Slug},
		{label: "title", value: label.OrDash(task.Title)},
		{label: "agents", value: label.OrDash(strings.Join(names, ", "))},
	}
	if isTaskWriter(writer.Agent) {
		fields = append(fields, recordField{label: "writer", value: writer.Agent.Name})
	}

	directory := label.OrDash(task.Cwd)
	if task.WorktreeRemoved {
		directory = "— (removed)"
	}
	fields = append(fields, recordField{label: "directory", value: directory})

	if writer.Agent.Branch != nil {
		fields = append(fields, recordField{label: branchFieldKey, value: *writer.Agent.Branch})
		fields = append(fields, pullRequestFields(writer.PullRequest, writer.PullRequests)...)
	}
	if params.read {
		fields = append(fields, worktreeStandingFields(params.worktree, writer.Agent.ID, params.now)...)
	}

	fields = append(fields, recordField{label: "task_id", value: task.ID})
	if created := label.Friendly(task.CreatedAt); created != "" {
		fields = append(fields, recordField{label: "created", value: created})
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

// agentsOnTasks is the live agents on any of the tasks, hidden ones included.
func (l listing) agentsOnTasks(tasks []panel.Task) []panel.Agent {
	ids := taskIDs(tasks)
	onTasks := xslice.Filter(l.agents, func(item panel.AgentRow) bool { return slices.Contains(ids, item.Agent.TaskID) })

	return xslice.Map(onTasks, func(item panel.AgentRow) panel.Agent { return item.Agent })
}

// findTask is the agent's task, or one spelled from the agent when the listing has none.
func (l listing) findTask(agent panel.Agent) panel.Task {
	return findAgentTask(l.tasks, agent)
}

// findAgentTask is the agent's task among the tasks, or one spelled from the agent when they
// hold none.
func findAgentTask(tasks map[string]panel.Task, agent panel.Agent) panel.Task {
	if task, ok := tasks[agent.TaskID]; ok {
		return task
	}

	return panel.Task{ID: agent.TaskID, WorkflowID: agent.WorkflowID, Slug: agent.TaskSlug}
}

// findTaskWriter is the agent that provisioned the task's checkout, or the lead when none on the
// list did.
func (l listing) findTaskWriter(task panel.Task, lead panel.AgentRow) panel.AgentRow {
	at := slices.IndexFunc(l.agents, func(item panel.AgentRow) bool {
		return item.Agent.TaskID == task.ID && isTaskWriter(item.Agent)
	})
	if at < 0 {
		return lead
	}

	return l.agents[at]
}

// findCheckoutWriter is the task's writer while it works in the task's own checkout.
func (l listing) findCheckoutWriter(task panel.Task, lead panel.AgentRow) (panel.AgentRow, bool) {
	writer := l.findTaskWriter(task, lead)
	if task.Cwd == "" || writer.Agent.Cwd != task.Cwd {
		return panel.AgentRow{}, false
	}

	return writer, true
}

// isTaskWriter reports whether the agent is the one its task's checkout was provisioned for.
func isTaskWriter(agent panel.Agent) bool {
	return agent.ParentAgentID == nil && !agent.ProvisionDisabled
}

// findLiveWriter is the top-level implementer working in the task's checkout.
func (l listing) findLiveWriter(taskID string) (panel.Agent, bool) {
	at := slices.IndexFunc(l.agents, func(item panel.AgentRow) bool {
		agent := item.Agent

		return agent.TaskID == taskID && agent.ParentAgentID == nil && agent.ArchivedAt == nil &&
			agent.Role == panel.RoleImplementer && agent.State != panel.StateFailedToCreate
	})
	if at < 0 {
		return panel.Agent{}, false
	}

	return l.agents[at].Agent, true
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
	form, confirmed := deleteTasksForm(tasks, len(m.listing.agentsOnTasks(tasks)), fixedKeys())

	return m.openForm(formState{
		kind:   formArchiveDelete,
		form:   form,
		update: (*model).updateForm,
		submit: holdSubmit(func(m *model) tea.Cmd {
			if !*confirmed {
				return nil
			}

			return m.deleteTasks(tasks)
		}),
	})
}

// isEveryAgentHidden reports whether there are agents and every one of them is hidden.
func isEveryAgentHidden(agents []panel.Agent) bool {
	return len(agents) > 0 && !slices.ContainsFunc(agents, func(agent panel.Agent) bool { return !agent.Hidden })
}

// hideAgents hides the agents, or unhides them when they are all hidden.
func hideAgents(m *model, agents []panel.Agent, subject string) tea.Cmd {
	hidden := !isEveryAgentHidden(agents)
	changed := xslice.Filter(agents, func(agent panel.Agent) bool { return agent.Hidden != hidden })

	doing, failure := "hiding "+subject, "Could not hide "+subject
	if !hidden {
		doing, failure = "unhiding "+subject, "Could not unhide "+subject
	}

	return m.writeCmd(doing, failure, func(a api) error {
		for _, agent := range changed {
			if err := a.setHidden(agent, hidden); err != nil {
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

			params := updateTaskParams{id: task.ID, title: &title, goal: &goal}
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
			Description(fmt.Sprintf("what is done when this task is; %s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).
			Validate(checkTaskGoal).Value(&values.Goal),
		textField("notes").Title("Notes").
			Description(fmt.Sprintf("what has been found and decided; %s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).
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

// splitChosen sorts the chosen agents into the tasks they wholly cover and the agents left over.
func splitChosen(l listing, chosen []panel.Agent) chosenWork {
	onTasks := xslice.Filter(chosen, func(agent panel.Agent) bool { return agent.TaskID != "" })
	unique := xslice.UniqueBy(onTasks, func(agent panel.Agent) string { return agent.TaskID })

	return splitAcross(l, chosen, xslice.Map(unique, l.findTask))
}

// splitAcross sorts the chosen agents into the candidate tasks they wholly cover and the agents left over.
func splitAcross(l listing, chosen []panel.Agent, candidates []panel.Task) chosenWork {
	ids := panel.IDs(chosen)
	isUnchosen := func(agent panel.Agent) bool { return !slices.Contains(ids, agent.ID) }

	wholeTasks := xslice.Filter(candidates, func(task panel.Task) bool {
		return !slices.ContainsFunc(l.agentsOnTasks([]panel.Task{task}), isUnchosen)
	})

	whole := taskIDs(wholeTasks)
	loneAgents := xslice.Filter(chosen, func(agent panel.Agent) bool { return !slices.Contains(whole, agent.TaskID) })

	return chosenWork{chosen: chosen, wholeTasks: wholeTasks, loneAgents: loneAgents}
}

// rechecked is the work with each task an agent has joined since handed back to its chosen agents.
func (w chosenWork) rechecked(l listing) chosenWork {
	return splitAcross(l, w.chosen, w.wholeTasks)
}

// affected is every live agent the work takes.
func (w chosenWork) affected(l listing) []panel.Agent {
	return slices.Concat(l.agentsOnTasks(w.wholeTasks), w.loneAgents)
}
