package ui

import (
	"fmt"
	"strings"

	workflowstore "weave/internal/store/workflow"
	"weave/lib/xslice"
	hookv1 "weave/proto/gen/hook/v1"

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
	case isBound(k, m.keys.Workflows.Adopt):
		return m.openAdopt(&workflow)
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
	values := workflowFormValues{}

	return m.openWorkflowConfigForm(&values, nil)
}

func (m model) renameWorkflowForm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	values := workflowFormValues{}

	return m.openWorkflowRename(workflow, &values)
}

func (m model) openWorkflowRename(workflow workflowstore.Workflow, values *workflowFormValues) (tea.Model, tea.Cmd) {
	title := fmt.Sprintf("Rename %q to", workflow.Name)

	return m.openRedrawnForm(
		formWorkflow,
		func(m model) *huh.Form { return workflowForm(title, values, m.keys.Form) },
		func(m model) (tea.Model, tea.Cmd) {
			renamed := workflow

			trimmed := strings.TrimSpace(values.Name)
			if trimmed == "" || trimmed == workflow.Name {
				return m.closeForm()
			}

			renamed.Name = trimmed
			cmd := m.writeCmd(
				fmt.Sprintf("Could not rename workflow %q", workflow.Name),
				func(a api) error { return a.updateWorkflow(renamed) },
			)

			return m, tea.Batch(cmd, m.holdForm())
		},
	)
}

func (m model) editWorkflowForm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	if fresh, err := m.api.getWorkflow(workflow.ID); err == nil && fresh != nil {
		workflow = *fresh
	}

	values := workflowFormValuesFrom(workflow.Config())

	return m.openWorkflowConfigForm(&values, &workflow)
}

// openWorkflowConfigForm puts up the form for a workflow's whole configuration, creating one when
// target is nil and saving over the workflow it names otherwise, and is the one form that redraws
// itself as the provisioner is switched under it.
func (m model) openWorkflowConfigForm(values *workflowFormValues, target *workflowstore.Workflow) (tea.Model, tea.Cmd) {
	title := "New workflow name"
	if target != nil {
		title = "Workflow name"
	}

	build := func(m model) (*huh.Form, int) {
		return workflowConfigForm(workflowConfigParams{
			NameTitle: title,
			Values:    values,
			Hooks:     m.getAvailableHooks(),
			Keys:      m.keys.Form,
		})
	}

	return m, m.openForm(formState{
		kind: formWorkflow,
		update: func(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
			return m.updateWorkflowForm(msg, values, build)
		},
		submit: func(m model) (tea.Model, tea.Cmd) {
			var cmd tea.Cmd
			if target == nil {
				cmd = m.createWorkflowCmd(values.toConfig())
			} else {
				edited := values.toConfig().Workflow(target.ID)
				cmd = m.writeCmd(
					fmt.Sprintf("Could not save workflow %q", target.Name),
					func(a api) error { return a.updateWorkflow(edited) },
				)
			}

			return m, tea.Batch(cmd, m.holdForm())
		},
		rebuild: func(m model) *huh.Form {
			form, _ := build(m)

			return form
		},
	})
}

// swapProvisioner redraws the form around the provisioner just chosen and puts the cursor back
// on the field that chose it, since the fields under it are not the ones that were there.
func (m model) swapProvisioner(build func(model) (*huh.Form, int)) (model, tea.Cmd) {
	form, toggleIndex := build(m)

	cmds := []tea.Cmd{form.Init()}
	for range toggleIndex {
		cmds = append(cmds, form.NextField())
	}

	m.form.form = form

	return m, tea.Batch(cmds...)
}

// getAvailableHooks is the hooks directory as the form offers it, and a directory that is not
// there reads as an empty list rather than an error, since a person who has written no hooks
// has nothing wrong with their setup.
func (m model) getAvailableHooks() []hookChoice {
	listed, err := m.api.listHooks()
	if err != nil {
		return nil
	}

	return xslice.Map(listed, func(hook *hookv1.Hook) hookChoice {
		return hookChoice{name: hook.GetName()}
	})
}

func (m model) archiveWorkflowConfirm(workflow workflowstore.Workflow) (tea.Model, tea.Cmd) {
	form, confirmed := archiveWorkflowForm(workflow.Name, m.keys.Form)

	return m, m.openForm(formState{
		kind:   formWorkflowArchive,
		form:   form,
		update: model.updateForm,
		submit: confirmSubmit(func(m model) (model, tea.Cmd) {
			if !*confirmed {
				return m, nil
			}

			return m, m.writeCmd(
				fmt.Sprintf("Could not archive workflow %q", workflow.Name),
				func(a api) error { return a.archiveWorkflow(workflow.ID) },
			)
		}),
	})
}

// updateWorkflowForm is the one form whose fields change as it is answered, so it watches the
// provisioner across the message that may have switched it.
func (m model) updateWorkflowForm(msg tea.Msg, values *workflowFormValues, build func(model) (*huh.Form, int)) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && isBound(key.String(), m.keys.Form.Submit) {
		return m.answerForm()
	}

	was := values.Provisioner

	form, cmd := m.form.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form.form = updated
	}

	switch m.form.form.State {
	case huh.StateCompleted:
		return m.answerForm()
	case huh.StateAborted:
		return m.closeForm()
	case huh.StateNormal:
	}

	if values.Provisioner != was {
		var swapped tea.Cmd
		m, swapped = m.swapProvisioner(build)
		cmd = tea.Batch(cmd, swapped)
	}

	return m, cmd
}
