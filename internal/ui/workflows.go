package ui

import (
	"fmt"

	"komrad/internal/eventprompts"
	"komrad/internal/ui/panel"
	"komrad/lib/xslice"
	hookv1 "komrad/proto/gen/hook/v1"

	tea "charm.land/bubbletea/v2"
)

func newWorkflowForm(m *model) tea.Cmd {
	values := workflowFormValues{
		EventPrompts: eventPromptsByEvent(xslice.Map(eventprompts.All(), func(event eventprompts.Event) panel.EventPrompt {
			return panel.EventPrompt{Event: event.Name, Prompt: event.DefaultPrompt}
		})),
	}
	if len(m.listing.workflows) == 0 {
		values.Cwd = m.getHostPath()
	}

	return openWorkflowConfigForm(m, &values, nil)
}

func editWorkflowForm(m *model, workflow panel.Workflow) tea.Cmd {
	values := workflowFormValuesFrom(workflow.Config())

	return openWorkflowConfigForm(m, &values, &workflow)
}

// openWorkflowConfigForm puts up the form for a workflow's whole configuration, creating one
// when target is nil and saving over the workflow it names otherwise.
func openWorkflowConfigForm(m *model, values *workflowFormValues, target *panel.Workflow) tea.Cmd {
	title := "New workflow name"
	if target != nil {
		title = "Workflow name"
	}

	return m.openForm(workflowForm(workflowFormParams{
		NameTitle: title,
		Values:    values,
		Editing:   target != nil,
		Submit: holdSubmit(func(m *model) tea.Cmd {
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

			return cmd
		}),
	}).topState())
}

// getAvailableHooks is the hooks directory as the form offers it, a directory that is not there
// reading as an empty list rather than an error.
func (m *model) getAvailableHooks() []string {
	listed, err := m.api.listHooks()
	if err != nil {
		return nil
	}

	return xslice.Map(listed, func(hook *hookv1.Hook) string {
		return hook.GetName()
	})
}

func (m *model) archiveWorkflow(workflow panel.Workflow) tea.Cmd {
	m.releasePanes(m.listing.agentsOf(workflow.ID))

	return m.writeCmd(
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

// eventPromptEditables are the event prompts the panel's editor key can open.
func eventPromptEditables(values *workflowFormValues) []editableText {
	return xslice.Map(eventprompts.All(), func(event eventprompts.Event) editableText {
		return editableText{key: eventPromptKey(event.Name), value: values.eventPromptFor(event.Name)}
	})
}
