package ui

import (
	"fmt"
	"slices"

	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
	hookv1 "komrad/proto/gen/hook/v1"

	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
)

func newWorkflowForm(h host, s panelShared) tea.Cmd {
	values := workflowFormValues{}
	if len(s.listing.workflows) == 0 {
		values.Cwd = h.getHostPath()
	}

	return openWorkflowConfigForm(h, &values, nil)
}

func editWorkflowForm(h host, _ panelShared, workflow panel.Workflow) tea.Cmd {
	values := workflowFormValuesFrom(workflow.Config())

	return openWorkflowConfigForm(h, &values, &workflow)
}

// openWorkflowConfigForm puts up the form for a workflow's whole configuration, creating one
// when target is nil and saving over the workflow it names otherwise.
func openWorkflowConfigForm(h host, values *workflowFormValues, target *panel.Workflow) tea.Cmd {
	title := "New workflow name"
	if target != nil {
		title = "Workflow name"
	}

	build := func(m model) (*huh.Form, []string) {
		return workflowConfigForm(workflowConfigParams{
			NameTitle: title,
			Values:    values,
			Hooks:     m.getAvailableHooks(),
			Keys:      formKeys(),
			Editing:   target != nil,
		})
	}

	return h.openForm(formState{
		kind:           formWorkflow,
		keepsOnRefusal: true,
		update: func(m model, msg tea.Msg) (tea.Model, tea.Cmd) {
			return m.updateWorkflowForm(msg, values, build)
		},
		editable: []editableText{
			{key: "context", value: &values.Context},
			{key: "worktree_copy_paths", value: &values.WorktreeCopyPaths},
			{key: "worktree_symlink_paths", value: &values.WorktreeSymlinkPaths},
			{key: "worktree_diff_exclude", value: &values.WorktreeDiffExclude},
		},
		submit: holdSubmit(func(m model) (model, tea.Cmd) {
			var cmd tea.Cmd
			if target == nil {
				cmd = m.createWorkflowCmd(values.toConfig())
			} else {
				edited := values.toConfig().Workflow(target.ID)
				cmd = m.writeCmd(
					"saving "+target.Name,
					fmt.Sprintf("Could not save workflow %q", target.Name),
					func(a api) error { return a.updateWorkflow(edited) },
				)
			}

			return m, cmd
		}),
		rebuild: build,
	})
}

// swapProvisioner redraws the form around the provisioner just chosen and puts the cursor back
// on the field that chose it.
func (m model) swapProvisioner(build func(model) (*huh.Form, []string)) (model, tea.Cmd) {
	form, order := build(m)

	// Redrawn from nothing, so it is told its own column again along with everything else.
	cmds := []tea.Cmd{form.Init(), formResized()}
	for range max(slices.Index(order, provisionerKey), 0) {
		cmds = append(cmds, form.NextField())
	}

	m.form.form = form
	m.form.order = order

	return m, tea.Batch(cmds...)
}

// getAvailableHooks is the hooks directory as the form offers it, a directory that is not there
// reading as an empty list rather than an error.
func (m model) getAvailableHooks() []hookChoice {
	listed, err := m.api.listHooks()
	if err != nil {
		return nil
	}

	return xslice.Map(listed, func(hook *hookv1.Hook) hookChoice {
		return hookChoice{name: hook.GetName()}
	})
}

func (m model) archiveWorkflow(workflow panel.Workflow) (model, tea.Cmd) {
	m.releasePanes(m.listing.agentsOf(workflow.ID))

	return m, m.writeCmd(
		"archiving "+workflow.Name,
		fmt.Sprintf("Could not archive workflow %q", workflow.Name),
		func(a api) error { return a.archiveWorkflow(workflow.ID) },
	)
}

// agentsOf is the agents the last listing found inside a workflow.
func (l listing) agentsOf(workflowID string) []panel.Agent {
	var held []panel.Agent
	for _, item := range l.agents {
		if item.Agent.WorkflowID == workflowID {
			held = append(held, item.Agent)
		}
	}

	return held
}

// updateWorkflowForm is the one form whose fields change as it is answered, so it watches the
// provisioner across the message that may have switched it.
func (m model) updateWorkflowForm(msg tea.Msg, values *workflowFormValues, build func(model) (*huh.Form, []string)) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && isBound(key.String(), formKeys().Submit) {
		return m.answerForm()
	}

	if key, ok := msg.(tea.KeyPressMsg); ok {
		if next, cmd, kept := m.updateFormVim(key); kept {
			return next, cmd
		}
	}

	was := values.Provisioner

	cmd := m.stepForm(msg)

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
