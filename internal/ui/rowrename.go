package ui

import (
	"fmt"

	"komrad/internal/ui/field"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// rowRename is a listed row's name being typed over: an agent's, a task's slug, a workflow's or a
// schedule's.
type rowRename struct {
	*field.NameEdit
	agent    panel.Agent
	task     *panel.Task
	workflow *panel.Workflow
	schedule *panel.Schedule
}

func renameAgentRow(row agentRow) *rowRename {
	if row.isTask() {
		task := *row.task

		return &rowRename{NameEdit: field.NewNameEdit(rowKey(row), task.Slug), task: &task}
	}
	if row.header {
		workflow := row.workflow

		return &rowRename{NameEdit: field.NewNameEdit(rowKey(row), workflow.Name), workflow: &workflow}
	}

	return &rowRename{NameEdit: field.NewNameEdit(rowKey(row), row.item.Agent.Name), agent: row.item.Agent}
}

func renameScheduleRow(item scheduleItem) *rowRename {
	schedule := item.schedule

	return &rowRename{NameEdit: field.NewNameEdit(item.Key(), schedule.Name), schedule: &schedule}
}

// save sends the typed name, or says there is nothing to send.
func (r *rowRename) save(m *model) (screen.Request, bool) {
	name, ok := r.Submit()
	if !ok {
		return nil, false
	}

	switch {
	case r.schedule != nil:
		renamed := schedulePrefill(*r.schedule)
		renamed.Name = name
		id := r.schedule.ID

		return m.rename(
			"renaming "+r.Was(),
			fmt.Sprintf("Could not rename schedule %q", r.Was()),
			r.Key(),
			func(a api) error { return a.updateSchedule(id, renamed) },
		), true
	case r.task != nil:
		id := r.task.ID

		return m.rename(
			"renaming "+r.Was(),
			fmt.Sprintf("Could not rename task %q", r.Was()),
			r.Key(),
			func(a api) error { return a.updateTask(updateTaskParams{id: id, slug: &name}) },
		), true
	case r.workflow != nil:
		renamed := *r.workflow
		renamed.Name = name

		return m.rename(
			"renaming "+r.Was(),
			fmt.Sprintf("Could not rename workflow %q", r.Was()),
			r.Key(),
			func(a api) error { return a.updateWorkflow(renamed) },
		), true
	}

	agent := r.agent

	return m.rename(
		"renaming "+r.Was(),
		fmt.Sprintf("Could not rename agent %q", r.Was()),
		r.Key(),
		func(a api) error { return a.renameAgent(agent, name) },
	), true
}

// renameSlot is the rename a list screen has open, nil when none is.
type renameSlot struct {
	renaming *rowRename
}

func (r *renameSlot) isRenaming() bool {
	return r.renaming != nil && !r.renaming.IsLanded()
}

func (r *renameSlot) renamingField() *field.NameEdit {
	if r.renaming == nil {
		return nil
	}

	return r.renaming.NameEdit
}

// reconcileRename drops an open rename once the cursor leaves its row.
func reconcileRename[T list.Keyed](r *renameSlot, rows []T, hovered string, name func(T) string) {
	if r.renaming != nil && !reconcileNameEdit(r.renaming.NameEdit, rows, hovered, name) {
		r.renaming = nil
	}
}

func (r *renameSlot) updateRename(m *model, msg tea.Msg) screen.Request {
	switch r.renaming.Take(msg, m.register) {
	case field.NameGone:
		return screen.NewNote(screen.Warning, r.renaming.GoneWarning())
	case field.NameHidden:
		return screen.Hide{}
	case field.NameCancelled:
		r.renaming = nil
	case field.NameSubmitted:
		request, ok := r.renaming.save(m)
		if !ok {
			r.renaming = nil
		}

		return request
	case field.NameTyping:
	}

	return nil
}

// settleField marks the field saved once its write has landed, or open for another try when it
// failed, and says whether there is a field to reconcile.
func (r *renameSlot) settleField(row string, err error) bool {
	if r.renaming == nil {
		return false
	}
	r.renaming.Settle(row, err)

	return true
}

func (r *renameSlot) typedLine() (field.Line, bool) {
	if !r.isRenaming() {
		return field.Line{}, false
	}

	return r.renaming.Input(), true
}
