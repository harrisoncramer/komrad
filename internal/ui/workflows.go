package ui

import (
	"fmt"
	"strings"

	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// updateHoveredWorkflow handles keys while the agents-tree cursor is on a workflow header.
// The workflow bindings act on that folder's workflow, while nav still moves the tree
// cursor. The workflow is resolved once and handed to whatever the key opens, so nothing
// has to hold which folder was hovered between keypresses.
func (m model) updateHoveredWorkflow(key tea.KeyMsg, workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case bound(k, m.keys.Agents.Quit):
		return m, m.hide()
	case bound(k, m.keys.Agents.Back):
		return m, m.hide()
	case bound(k, m.keys.Nav.Up):
		if m.tree.cursor > 0 {
			m.tree.cursor--
			m.stageSelection()
		}
	case bound(k, m.keys.Nav.Down):
		if m.tree.cursor < len(m.agentRows())-1 {
			m.tree.cursor++
			m.stageSelection()
		}
	case bound(k, m.keys.Agents.New):
		return m.openNewAgent(&workflow)
	case bound(k, m.keys.Workflows.Rename):
		return m.renameWorkflowForm(workflow)
	case bound(k, m.keys.Workflows.Edit):
		return m.editWorkflowForm(workflow)
	case bound(k, m.keys.Workflows.Delete):
		return m.deleteWorkflowConfirm(workflow)
	}

	return m, nil
}

// returnToAgents sends the user back to the agents view once a form, dialog, or other
// screen closes, restaging for the current selection.
func (m model) returnToAgents() (tea.Model, tea.Cmd) {
	m.screen = screenAgents
	m.tree.cursor = clampCursor(m.tree.cursor, len(m.agentRows()))
	m.stageSelection()

	return m, nil
}

// newWorkflowForm opens the form to create a workflow: a name plus the default cwd and
// flags that seed the agents created inside it.
func (m model) newWorkflowForm() (tea.Model, tea.Cmd) {
	m.target.renameWorkflow = nil
	m.target.editWorkflow = nil
	m.form, m.values.workflow = workflowConfigForm("New workflow name", workflowstore.Config{}, m.availableActions(), m.keys.Form)
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

// renameWorkflowForm opens the single-input form to rename the highlighted workflow.
func (m model) renameWorkflowForm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	m.target.renameWorkflow = &workflow
	m.target.editWorkflow = nil
	m.form, m.values.workflow = workflowForm(fmt.Sprintf("Rename %q to", workflow.Name), m.keys.Form)
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

// editWorkflowForm reopens the create form for the highlighted workflow, prefilled so its
// properties can be patched in place. It refetches so the form reflects what is stored,
// falling back to the listed row if that fails.
func (m model) editWorkflowForm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	if fresh, err := m.api.getWorkflow(workflow.ID); err == nil && fresh != nil {
		workflow = *fresh
	}
	m.target.editWorkflow = &workflow
	m.target.renameWorkflow = nil
	m.form, m.values.workflow = workflowConfigForm("Workflow name", workflowstore.Config{
		Name:          workflow.Name,
		Context:       workflow.Context,
		Harness:       workflow.Harness,
		Cwd:           workflow.Cwd,
		Flags:         workflow.Flags,
		SetupAction:   workflow.SetupAction,
		CleanupAction: workflow.CleanupAction,
	}, m.availableActions(), m.keys.Form)
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

// availableActions is what the actions directory holds, which the workflow form's two
// action fields are chosen from. It is fetched on the render loop because a form has
// nothing to open on until the answer is in, and a listing that fails leaves the fields
// offering none plus whatever the workflow already records.
func (m model) availableActions() []string {
	listed, err := m.api.listActions()
	if err != nil {
		return nil
	}

	names := make([]string, 0, len(listed))
	for _, action := range listed {
		names = append(names, action.GetName())
	}

	return names
}

// deleteWorkflowConfirm opens the delete prompt for the highlighted workflow,
// asking whether its agents should be removed with it.
func (m model) deleteWorkflowConfirm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	m.target.deleteWorkflow = &workflow
	m.form, m.values.confirmed = deleteWorkflowForm(workflow.Name, m.keys.Form)
	m.screen = screenWorkflowDelete

	return m, m.form.Init()
}

// updateWorkflowDelete drives the delete prompt. Confirming purges the workflow with its
// agents, and anything else leaves it untouched. All outcomes return to the agents view.
func (m model) updateWorkflowDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		var write tea.Cmd
		if target := m.target.deleteWorkflow; target != nil && m.wasConfirmed() {
			write = m.writeCmd(
				fmt.Sprintf("Could not delete workflow %q", target.Name),
				func(a api) error { return a.deleteWorkflow(target.ID) },
			)
		}
		m.target.deleteWorkflow = nil
		m.values.confirmed = nil
		returned, refresh := m.returnToAgents()

		return returned, tea.Batch(write, refresh)
	case huh.StateAborted:
		m.target.deleteWorkflow = nil
		m.values.confirmed = nil
		return m.returnToAgents()
	default:
		return m, cmd
	}
}

// updateWorkflowForm drives the create/rename form, submitting on the configured
// submit key or on completion and aborting back to the agents view.
func (m model) updateWorkflowForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && bound(key.String(), m.keys.Form.Submit) {
		return m.submitWorkflowForm()
	}

	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		return m.submitWorkflowForm()
	case huh.StateAborted:
		m.target.renameWorkflow = nil
		m.target.editWorkflow = nil
		m.values.workflow = nil
		return m.returnToAgents()
	default:
		return m, cmd
	}
}

// submitWorkflowForm applies the form, renaming a workflow, editing its config, or
// creating one, then returns to the refreshed agents view.
func (m model) submitWorkflowForm() (tea.Model, tea.Cmd) {
	// The form writes into values the panel handed it, so a submit without them is a
	// submit for a form that was never opened.
	if m.values.workflow == nil {
		return m.returnToAgents()
	}

	values := *m.values.workflow

	var write tea.Cmd
	switch {
	case m.target.renameWorkflow != nil:
		// A rename is an update carrying the fields it is not changing, since the
		// service replaces all of them.
		target := *m.target.renameWorkflow
		if trimmed := strings.TrimSpace(values.Name); trimmed != "" && trimmed != target.Name {
			target.Name = trimmed
			write = m.writeCmd(
				fmt.Sprintf("Could not rename workflow %q", m.target.renameWorkflow.Name),
				func(a api) error { return a.updateWorkflow(target) },
			)
		}
	case m.target.editWorkflow != nil:
		edited := workflowstore.Workflow{
			ID:            m.target.editWorkflow.ID,
			Name:          values.Name,
			Context:       values.Context,
			Harness:       values.Harness,
			Cwd:           values.Cwd,
			Flags:         values.Flags,
			SetupAction:   values.SetupAction,
			CleanupAction: values.CleanupAction,
		}
		write = m.writeCmd(
			fmt.Sprintf("Could not save workflow %q", m.target.editWorkflow.Name),
			func(a api) error { return a.updateWorkflow(edited) },
		)
	default:
		write = m.writeCmd("Could not create the workflow", func(a api) error { return a.createWorkflow(values) })
	}

	m.target.renameWorkflow = nil
	m.target.editWorkflow = nil
	m.values.workflow = nil
	returned, refresh := m.returnToAgents()

	return returned, tea.Batch(write, refresh)
}
