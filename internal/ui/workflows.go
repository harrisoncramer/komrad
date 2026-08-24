package ui

import (
	"fmt"
	"strings"

	workflowstore "weave/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

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

func (m model) returnToAgents() (tea.Model, tea.Cmd) {
	m.enterScreen(screenAgents)
	m.tree.cursor = clampCursor(m.tree.cursor, len(m.agentRows()))
	m.stageSelection()

	return m, nil
}

func (m model) newWorkflowForm() (tea.Model, tea.Cmd) {
	m.target.renameWorkflow = nil
	m.target.editWorkflow = nil
	m.form, m.values.workflow, _ = m.workflowFormFor(workflowFormValues{})
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

func (m model) renameWorkflowForm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	m.target.renameWorkflow = &workflow
	m.target.editWorkflow = nil
	m.form, m.values.workflow, _ = m.workflowFormFor(workflowFormValues{})
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

func (m model) editWorkflowForm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	if fresh, err := m.api.getWorkflow(workflow.ID); err == nil && fresh != nil {
		workflow = *fresh
	}
	m.target.editWorkflow = &workflow
	m.target.renameWorkflow = nil
	m.form, m.values.workflow, _ = m.workflowFormFor(workflowFormValuesFrom(workflowstore.Config{
		Name:          workflow.Name,
		Context:       workflow.Context,
		Harness:       workflow.Harness,
		Cwd:           workflow.Cwd,
		Flags:         workflow.Flags,
		SetupAction:   workflow.SetupAction,
		CleanupAction: workflow.CleanupAction,
		Worktree:      workflow.Worktree,
	}))
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

func (m model) workflowFormFor(values workflowFormValues) (*huh.Form, *workflowFormValues, int) {
	switch {
	case m.target.renameWorkflow != nil:
		form, out := workflowForm(fmt.Sprintf("Rename %q to", m.target.renameWorkflow.Name), values, m.keys.Form)

		return form, out, -1
	case m.target.editWorkflow != nil:
		return workflowConfigForm("Workflow name", values, m.getAvailableActions(), m.keys.Form)
	default:
		return workflowConfigForm("New workflow name", values, m.getAvailableActions(), m.keys.Form)
	}
}

func (m model) rebuildWorkflowForm() (model, tea.Cmd) {
	if m.values.workflow == nil {
		return m, nil
	}

	form, values, toggleIndex := m.workflowFormFor(*m.values.workflow)

	cmds := []tea.Cmd{form.Init()}
	for range toggleIndex {
		cmds = append(cmds, form.NextField())
	}

	m.form = form
	m.values.workflow = values

	return m, tea.Batch(cmds...)
}

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

func (m model) archiveWorkflowConfirm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	m.target.archiveWorkflow = &workflow
	m.form, m.values.confirmed = archiveWorkflowForm(workflow.Name, m.keys.Form)
	m.screen = screenWorkflowArchive

	return m, m.form.Init()
}

func (m model) updateWorkflowArchive(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m.updateConfirmForm(msg, confirmFormActions{
		write: func(m model) tea.Cmd {
			target := m.target.archiveWorkflow
			if target == nil || !m.wasConfirmed() {
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

func (m model) updateWorkflowForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && isBound(key.String(), m.keys.Form.Submit) {
		return m.submitWorkflowForm()
	}

	if m.values.workflow == nil {
		return m.closeForm()
	}
	wasSetupType := m.values.workflow.SetupType

	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		return m.submitWorkflowForm()
	case huh.StateAborted:
		return m.closeForm()
	case huh.StateNormal:
	}

	if m.values.workflow.SetupType != wasSetupType {
		var rebuildCmd tea.Cmd
		m, rebuildCmd = m.rebuildWorkflowForm()
		cmd = tea.Batch(cmd, rebuildCmd)
	}

	return m, cmd
}

func (m model) submitWorkflowForm() (tea.Model, tea.Cmd) {
	if m.values.workflow == nil {
		return m.closeForm()
	}

	values := *m.values.workflow

	var write tea.Cmd
	switch {
	case m.target.renameWorkflow != nil:

		target := *m.target.renameWorkflow
		if trimmed := strings.TrimSpace(values.Name); trimmed != "" && trimmed != target.Name {
			target.Name = trimmed
			write = m.writeCmd(
				fmt.Sprintf("Could not rename workflow %q", m.target.renameWorkflow.Name),
				func(a api) error { return a.updateWorkflow(target) },
			)
		}
	case m.target.editWorkflow != nil:
		cfg := values.toConfig()
		edited := workflowstore.Workflow{
			ID:            m.target.editWorkflow.ID,
			Name:          cfg.Name,
			Context:       cfg.Context,
			Harness:       cfg.Harness,
			Cwd:           cfg.Cwd,
			Flags:         cfg.Flags,
			SetupAction:   cfg.SetupAction,
			CleanupAction: cfg.CleanupAction,
			Worktree:      cfg.Worktree,
		}
		write = m.writeCmd(
			fmt.Sprintf("Could not save workflow %q", m.target.editWorkflow.Name),
			func(a api) error { return a.updateWorkflow(edited) },
		)
	default:
		write = m.createWorkflowCmd(values.toConfig())
	}

	if write == nil {
		return m.closeForm()
	}

	return m, tea.Batch(write, m.holdForm())
}
