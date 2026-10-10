package ui

import (
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

// taskAgents is the live agents on a task, oldest first.
func (l listing) taskAgents(taskID string) []panel.AgentRow {
	return xslice.Filter(l.agents, func(item panel.AgentRow) bool { return item.Agent.TaskID == taskID })
}

func taskFields(task panel.Task, agents []panel.AgentRow) []recordField {
	fields := []recordField{
		{label: "task", value: task.Slug},
		{label: "title", value: label.OrDash(task.Title)},
		{label: "agents", value: fmt.Sprintf("%d", len(agents))},
		{label: "task_id", value: task.ID},
	}
	if created := label.Friendly(task.CreatedAt); created != "" {
		fields = append(fields, recordField{label: "created", value: created})
	}

	return fields
}

// taskMarkdown is a task's goal with the agents working on it, each with its branch and pull
// request.
func taskMarkdown(task panel.Task, agents []panel.AgentRow) string {
	goal := task.Goal
	if strings.TrimSpace(goal) == "" {
		goal = "_no goal set_"
	}

	lines := xslice.Map(agents, func(item panel.AgentRow) string {
		parts := []string{item.Agent.Name}
		if item.Agent.Role != "" {
			parts = append(parts, string(item.Agent.Role))
		}
		if item.Agent.Branch != nil {
			parts = append(parts, "on `"+*item.Agent.Branch+"`")
		}
		if len(item.PullRequests) > 0 {
			current := item.PullRequests[0]
			parts = append(parts, current.Ref()+" "+pullRequestWords(current))
		}

		return "- " + strings.Join(parts, ", ")
	})

	return renderDocument(task.Title, "", goal, strings.Join(lines, "\n"))
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

// isTaskWriter reports whether the agent is the one its task's checkout was provisioned for.
func isTaskWriter(agent panel.Agent) bool {
	return agent.ParentAgentID == nil && !agent.ProvisionDisabled
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

// hideTasks hides every agent on the tasks, or unhides them when they are all hidden.
func hideTasks(m *model, tasks []panel.Task) tea.Cmd {
	agents := m.listing.agentsOnTasks(tasks)
	hidden := !isEveryAgentHidden(agents)
	changed := xslice.Filter(agents, func(agent panel.Agent) bool { return agent.Hidden != hidden })

	doing, failure := "hiding "+describeTasks(tasks), "Could not hide "+describeTasks(tasks)
	if !hidden {
		doing, failure = "unhiding "+describeTasks(tasks), "Could not unhide "+describeTasks(tasks)
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

type taskEditValues struct {
	Title string
	Goal  string
}

// openEditTask opens the form that edits a task's title and goal.
func openEditTask(m *model, task panel.Task) tea.Cmd {
	values := &taskEditValues{Title: task.Title, Goal: task.Goal}

	return m.openForm(rebuildableForm(rebuildableFormParams{
		Kind: formTask,
		Build: func(m *model) (*huh.Form, []string) {
			return editTaskForm(fixedKeys(), values, task.Slug)
		},
		Editable: []editableText{{key: "goal", value: &values.Goal}},
		Submit: holdSubmit(func(m *model) tea.Cmd {
			title, goal := strings.TrimSpace(values.Title), strings.TrimSpace(values.Goal)

			return m.writeCmd(
				"saving "+task.Slug,
				fmt.Sprintf("Could not save task %q", task.Slug),
				func(a api) error {
					if err := errors.Join(checkTaskTitle(title), checkTaskGoal(goal)); err != nil {
						return err
					}

					return a.updateTask(updateTaskParams{id: task.ID, title: &title, goal: &goal})
				},
			)
		}),
	}))
}

func editTaskForm(keys config.FixedKeys, values *taskEditValues, slug string) (*huh.Form, []string) {
	return formOf(keys,
		huh.NewInput().Key("title").Title(fmt.Sprintf("Task %q", slug)).Description("its title").
			CharLimit(maxTaskTitle).Validate(checkTaskTitle).Value(&values.Title),
		textField("goal").Title("Goal").
			Description(fmt.Sprintf("what is done when this task is; %s opens $EDITOR; %s submits", keyLabel(keys.Editor), keyLabel(keys.Submit))).
			Validate(checkTaskGoal).Value(&values.Goal),
	)
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

// chosenTasks is the tasks the chosen agents work on, each once.
func chosenTasks(l listing, agents []panel.Agent) []panel.Task {
	onTasks := xslice.Filter(agents, func(agent panel.Agent) bool { return agent.TaskID != "" })
	unique := xslice.UniqueBy(onTasks, func(agent panel.Agent) string { return agent.TaskID })

	return xslice.Map(unique, l.findTask)
}
