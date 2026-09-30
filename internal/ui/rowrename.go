package ui

import (
	"fmt"

	"komrad/internal/ui/panel"

	tea "charm.land/bubbletea/v2"
)

// rowRename is a listed row's name being typed over: an agent's, a workflow's or a schedule's.
type rowRename struct {
	*nameEdit
	agent    panel.Agent
	workflow *panel.Workflow
	schedule *panel.Schedule
}

func renameAgentRow(row agentRow) *rowRename {
	if row.header {
		workflow := row.workflow

		return &rowRename{nameEdit: openNameEdit(rowKey(row), workflow.Name), workflow: &workflow}
	}

	return &rowRename{nameEdit: openNameEdit(rowKey(row), row.item.Agent.Name), agent: row.item.Agent}
}

func renameScheduleRow(item scheduleItem) *rowRename {
	schedule := item.schedule

	return &rowRename{nameEdit: openNameEdit(item.key(), schedule.Name), schedule: &schedule}
}

// save sends the typed name, or says there is nothing to send.
func (r *rowRename) save(h host) (tea.Cmd, bool) {
	name, ok := r.submit()
	if !ok {
		return nil, false
	}

	switch {
	case r.schedule != nil:
		renamed := schedulePrefill(*r.schedule)
		renamed.Name = name
		id := r.schedule.ID

		return stampRename(h.writeCmd(
			"renaming "+r.was,
			fmt.Sprintf("Could not rename schedule %q", r.was),
			func(a api) error { return a.updateSchedule(id, renamed) },
		), r.key), true
	case r.workflow != nil:
		renamed := *r.workflow
		renamed.Name = name

		return stampRename(h.writeCmd(
			"renaming "+r.was,
			fmt.Sprintf("Could not rename workflow %q", r.was),
			func(a api) error { return a.updateWorkflow(renamed) },
		), r.key), true
	}

	agent := r.agent

	return stampRename(h.writeCmd(
		"renaming "+r.was,
		fmt.Sprintf("Could not rename agent %q", r.was),
		func(a api) error { return a.renameAgent(agent, name) },
	), r.key), true
}

// renameSlot is the rename a list screen has open, nil when none is.
type renameSlot struct {
	renaming *rowRename
}

func (r *renameSlot) isRenaming() bool {
	return r.renaming != nil && !r.renaming.landed
}

func (r *renameSlot) renamingField() *nameEdit {
	if r.renaming == nil {
		return nil
	}

	return r.renaming.nameEdit
}

// reconcileRename drops an open rename once the cursor leaves its row.
func reconcileRename[T keyed](r *renameSlot, rows []T, hovered string, name func(T) string) {
	if r.renaming != nil && !reconcileNameEdit(r.renaming.nameEdit, rows, hovered, name) {
		r.renaming = nil
	}
}

func (r *renameSlot) updateRename(h host, msg tea.Msg) tea.Cmd {
	switch r.renaming.take(h, msg) {
	case nameEditHidden:
		return h.hide()
	case nameEditCancelled:
		r.renaming = nil
	case nameEditSubmitted:
		cmd, ok := r.renaming.save(h)
		if !ok {
			r.renaming = nil
		}

		return cmd
	case nameEditTyping:
	}

	return nil
}

// settleField marks the field saved once its write has landed, or open for another try when it
// failed, and says whether there is a field to reconcile.
func (r *renameSlot) settleField(row string, err error) bool {
	if r.renaming == nil {
		return false
	}
	r.renaming.settle(row, err)

	return true
}
