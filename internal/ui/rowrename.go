package ui

import (
	"fmt"

	"komrad/internal/ui/api"
	"komrad/internal/ui/field"
	"komrad/internal/ui/list"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/internal/vim"

	tea "charm.land/bubbletea/v2"
)

// rowRename is a listed row's name being typed over: an agent's, a task's slug or a workflow's.
type rowRename struct {
	*field.NameEdit
	agent    panel.Agent
	task     *panel.Task
	workflow *panel.Workflow
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

// save sends the typed name, or says there is nothing to send.
func (r *rowRename) save(clients api.Clients) (screen.Request, bool) {
	name, ok := r.Submit()
	if !ok {
		return nil, false
	}

	switch {
	case r.task != nil:
		id := r.task.ID

		return clients.RenameWrite(api.RenameWriteParams{
			Doing:   "renaming " + r.Was(),
			Summary: fmt.Sprintf("Could not rename task %q", r.Was()),
			Row:     r.Key(),
			Write:   func(a api.Clients) error { return a.UpdateTask(api.UpdateTaskParams{ID: id, Slug: &name}) },
		}), true
	case r.workflow != nil:
		renamed := *r.workflow
		renamed.Name = name

		return clients.RenameWrite(api.RenameWriteParams{
			Doing:   "renaming " + r.Was(),
			Summary: fmt.Sprintf("Could not rename workflow %q", r.Was()),
			Row:     r.Key(),
			Write:   func(a api.Clients) error { return a.UpdateWorkflow(renamed) },
		}), true
	}

	agent := r.agent

	return clients.RenameWrite(api.RenameWriteParams{
		Doing:   "renaming " + r.Was(),
		Summary: fmt.Sprintf("Could not rename agent %q", r.Was()),
		Row:     r.Key(),
		Write:   func(a api.Clients) error { return a.RenameAgent(agent, name) },
	}), true
}

// renameSlot is the rename a list screen has open, nil when none is.
type renameSlot struct {
	renaming *rowRename
}

func (r *renameSlot) IsRenaming() bool {
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
	if r.renaming != nil && !field.ReconcileNameEdit(r.renaming.NameEdit, rows, hovered, name) {
		r.renaming = nil
	}
}

func (r *renameSlot) updateRename(register *vim.Register, clients api.Clients, msg tea.Msg) screen.Request {
	switch r.renaming.Take(msg, register) {
	case field.NameGone:
		return screen.NewNote(screen.Warning, r.renaming.GoneWarning())
	case field.NameHidden:
		return screen.Hide{}
	case field.NameCancelled:
		r.renaming = nil
	case field.NameSubmitted:
		request, ok := r.renaming.save(clients)
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

func (r *renameSlot) TypedLine() (field.Line, bool) {
	if !r.IsRenaming() {
		return field.Line{}, false
	}

	return r.renaming.Input(), true
}
