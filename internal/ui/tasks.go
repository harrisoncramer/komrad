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

// taskFieldsParams is a task's checkout with the agents and checkout owner its info panel shows.
type taskFieldsParams struct {
	checkout checkoutFieldsParams
	agents   []panel.AgentRow
	owner    panel.AgentRow
}

func taskFields(params taskFieldsParams) []recordField {
	task, owner := params.checkout.task, params.owner
	names := xslice.Map(params.agents, func(item panel.AgentRow) string { return item.Agent.Name })
	fields := []recordField{
		{label: "task", value: task.Slug},
		{label: "title", value: label.OrDash(task.Title)},
		{label: "agents", value: label.OrDash(strings.Join(names, ", "))},
	}
	if isCheckoutOwner(owner.Agent) {
		fields = append(fields, recordField{label: "writer", value: owner.Agent.Name})
	}

	directory := label.OrDash(task.Cwd)
	if task.WorktreeRemoved {
		directory = "— (removed)"
	}
	fields = append(fields, recordField{label: "directory", value: directory})
	fields = append(fields, checkoutFields(params.checkout)...)

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

// findTaskLead is the agent a task is opened through.
func (l listing) findTaskLead(task panel.Task) (panel.AgentRow, bool) {
	return findLead(l.taskAgents(task))
}

// findLead is the newest of the agents on a task no parent among them stands over, preferring
// one with a pane.
func findLead(onTask []panel.AgentRow) (panel.AgentRow, bool) {
	byID := keyAgentsByID(onTask)
	heads := xslice.Filter(onTask, func(item panel.AgentRow) bool {
		if item.Agent.ParentAgentID == nil {
			return true
		}
		_, onIt := byID[*item.Agent.ParentAgentID]

		return !onIt
	})
	if len(heads) == 0 {
		return panel.AgentRow{}, false
	}

	lead := heads[0]
	for _, head := range heads[1:] {
		if isBetterLead(head, lead) {
			lead = head
		}
	}

	return lead, true
}

// findCheckoutOwner is the agent that provisioned the task's checkout, or the task's lead when none
// on the list did.
func (l listing) findCheckoutOwner(task panel.Task) panel.AgentRow {
	onTask := l.taskAgents(task)
	at := slices.IndexFunc(onTask, func(item panel.AgentRow) bool { return isCheckoutOwner(item.Agent) })
	if at < 0 {
		lead, _ := l.findTaskLead(task)

		return lead
	}

	return onTask[at]
}

// findOwnerInCheckout is the task's checkout owner while it works in that checkout.
func (l listing) findOwnerInCheckout(task panel.Task) (panel.AgentRow, bool) {
	owner := l.findCheckoutOwner(task)
	if task.Cwd == "" || owner.Agent.Cwd != task.Cwd {
		return panel.AgentRow{}, false
	}

	return owner, true
}

// isCheckoutOwner reports whether the agent is the one its task's checkout was provisioned for.
func isCheckoutOwner(agent panel.Agent) bool {
	return agent.ParentAgentID == nil && !agent.ProvisionDisabled
}

// findLiveWriter is the writer working in the task's checkout.
func (l listing) findLiveWriter(taskID string) (panel.Agent, bool) {
	at := slices.IndexFunc(l.agents, func(item panel.AgentRow) bool {
		return item.Agent.TaskID == taskID && isLiveWriter(item.Agent)
	})
	if at < 0 {
		return panel.Agent{}, false
	}

	return l.agents[at].Agent, true
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

	return splitAcross(l, chosen, xslice.Map(unique, l.tasks.findForAgent))
}

// splitAcross sorts the chosen agents into the candidate tasks they wholly cover and the agents left over.
func splitAcross(l listing, chosen []panel.Agent, candidates []panel.Task) chosenWork {
	ids := panel.IDs(chosen)
	isUnchosen := func(agent panel.Agent) bool { return !slices.Contains(ids, agent.ID) }

	wholeTasks := xslice.Filter(candidates, func(task panel.Task) bool {
		onTask := xslice.Map(l.taskAgents(task), func(item panel.AgentRow) panel.Agent { return item.Agent })

		return len(onTask) > 0 && !slices.ContainsFunc(onTask, isUnchosen)
	})

	whole := taskIDs(wholeTasks)
	loneAgents := xslice.Filter(chosen, func(agent panel.Agent) bool { return !slices.Contains(whole, agent.TaskID) })

	return chosenWork{
		chosen:     chosen,
		wholeTasks: wholeTasks,
		loneAgents: loneAgents,
	}
}

// rechecked is the work with each task an agent has joined since handed back to its chosen agents.
func (w chosenWork) rechecked(l listing) chosenWork {
	return splitAcross(l, w.chosen, w.wholeTasks)
}

// affected is every live agent the work takes.
func (w chosenWork) affected(l listing) []panel.Agent {
	return slices.Concat(l.agentsOnTasks(w.wholeTasks), w.loneAgents)
}

// isBetterLead reports whether a task should stand for the head over the lead it has so far.
func isBetterLead(head, lead panel.AgentRow) bool {
	if hasPane(head) != hasPane(lead) {
		return hasPane(head)
	}

	return head.Agent.CreatedAt.After(lead.Agent.CreatedAt)
}
