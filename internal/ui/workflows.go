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
// cursor. The workflow is read once and handed to whatever the key opens, so nothing
// has to hold which folder was hovered between keypresses.
func (m model) updateHoveredWorkflow(key tea.KeyMsg, workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, m.keys.Agents.Quit):
		return m, m.hide()
	case isBound(k, m.keys.Agents.Back):
		return m, m.hide()
	case isBound(k, m.keys.Nav.Up):
		if m.tree.cursor > 0 {
			m.tree.cursor--
			m.stageSelection()
		}
	case isBound(k, m.keys.Nav.Down):
		if m.tree.cursor < len(m.agentRows())-1 {
			m.tree.cursor++
			m.stageSelection()
		}
	case isBound(k, m.keys.Agents.New):
		return m.openNewAgent(&workflow)
	case isBound(k, m.keys.Workflows.Rename):
		return m.renameWorkflowForm(workflow)
	case isBound(k, m.keys.Workflows.Edit):
		return m.editWorkflowForm(workflow)
	case isBound(k, m.keys.Workflows.Archive):
		return m.archiveWorkflowConfirm(workflow)
	}

	return m, nil
}

// returnToAgents sends the user back to the agents view once a form, dialog, or other
// screen closes, restaging for the current selection.
func (m model) returnToAgents() (tea.Model, tea.Cmd) {
	m.enterScreen(screenAgents)
	m.tree.cursor = clampCursor(m.tree.cursor, len(m.agentRows()))
	m.stageSelection()

	return m, nil
}

// newWorkflowForm opens the form to create a workflow: a name plus the default cwd and
// flags that seed the agents created inside it.
func (m model) newWorkflowForm() (tea.Model, tea.Cmd) {
	m.target.renameWorkflow = nil
	m.target.editWorkflow = nil
	m.form, m.values.workflow = m.workflowFormFor(workflowstore.Config{})
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

// renameWorkflowForm opens the single-input form to rename the highlighted workflow.
func (m model) renameWorkflowForm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	m.target.renameWorkflow = &workflow
	m.target.editWorkflow = nil
	m.form, m.values.workflow = m.workflowFormFor(workflowstore.Config{})
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
	m.form, m.values.workflow = m.workflowFormFor(workflowstore.Config{
		Name:          workflow.Name,
		Context:       workflow.Context,
		Harness:       workflow.Harness,
		Cwd:           workflow.Cwd,
		Flags:         workflow.Flags,
		SetupAction:   workflow.SetupAction,
		CleanupAction: workflow.CleanupAction,
	})
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

// workflowFormFor builds whichever workflow form the target says is open, around the given
// config. It is decided here rather than at each opener so a refused submit can put the
// same form back.
func (m model) workflowFormFor(cfg workflowstore.Config) (*huh.Form, *workflowstore.Config) {
	switch {
	case m.target.renameWorkflow != nil:
		return workflowForm(fmt.Sprintf("Rename %q to", m.target.renameWorkflow.Name), cfg, m.keys.Form)
	case m.target.editWorkflow != nil:
		return workflowConfigForm("Workflow name", cfg, m.getAvailableActions(), m.keys.Form)
	default:
		return workflowConfigForm("New workflow name", cfg, m.getAvailableActions(), m.keys.Form)
	}
}

// getAvailableActions is what the actions directory holds, which the workflow form's two
// action fields are chosen from. It is fetched on the render loop because a form has
// nothing to open on until the answer is in, and a listing that fails leaves the fields
// offering none plus whatever the workflow already records.
//
// Shell written to be reused by other actions is left out, the same as it is on the actions
// list. A workflow naming one would run a fragment on its own.
func (m model) getAvailableActions() []actionChoice {
	listed, err := m.api.listActions()
	if err != nil {
		return nil
	}

	choices := make([]actionChoice, 0, len(listed))
	for _, action := range listed {
		if isInternalAction(action.GetName()) {
			continue
		}

		choices = append(choices, actionChoice{name: action.GetName()})
	}

	return choices
}

// archiveWorkflowConfirm opens the archive prompt for the highlighted workflow,
// asking whether its agents should go with it.
func (m model) archiveWorkflowConfirm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	m.target.archiveWorkflow = &workflow
	m.form, m.values.confirmed = archiveWorkflowForm(workflow.Name, m.keys.Form)
	m.screen = screenWorkflowArchive

	return m, m.form.Init()
}

// updateWorkflowArchive drives the archive prompt. Confirming archives the workflow with
// its agents, and anything else leaves it untouched. All outcomes return to the agents view.
func (m model) updateWorkflowArchive(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateConfirmForm(msg, confirmFormActions{
		write: func(m model) tea.Cmd {
			target := m.target.archiveWorkflow
			if target == nil {
				return nil
			}

			return m.writeCmd(
				fmt.Sprintf("Could not archive workflow %q", target.Name),
				func(a api) error { return a.archiveWorkflow(target.ID) },
			)
		},
		clear: func(m model) model {
			m.target.archiveWorkflow = nil
			m.values.confirmed = nil

			return m
		},
		returnTo: func(m model) (tea.Model, tea.Cmd) {
			return m.returnToAgents()
		},
	})
}

// updateWorkflowForm drives the create/rename form, submitting on the configured
// submit key or on completion and aborting back to the agents view.
func (m model) updateWorkflowForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateSubmitForm(msg, model.submitWorkflowForm)
}

// submitWorkflowForm applies the form, renaming a workflow, editing its config, or
// creating one.
func (m model) submitWorkflowForm() (tea.Model, tea.Cmd) {
	// The form writes into values the panel handed it, so a submit without them is a
	// submit for a form that was never opened.
	if m.values.workflow == nil {
		return m.closeForm()
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
		write = m.createWorkflowCmd(values)
	}

	// A rename to the name it already has writes nothing, and a form with no write has
	// nothing to wait for.
	if write == nil {
		return m.closeForm()
	}

	return m, tea.Batch(write, m.holdForm())
}
