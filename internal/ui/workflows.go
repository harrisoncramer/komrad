package ui

import (
	"fmt"

	"komrad/internal/eventprompts"
	"komrad/internal/ui/form"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
	hookv1 "komrad/proto/gen/hook/v1"
)

func newWorkflowForm(m *model) screen.Request {
	values := workflowFormValues{
		EventPrompts: eventPromptsByEvent(xslice.Map(eventprompts.All(), func(event eventprompts.Event) panel.EventPrompt {
			return panel.EventPrompt{Event: event.Name, Prompt: event.DefaultPrompt}
		})),
	}
	if len(m.listing.Workflows) == 0 {
		values.Cwd = m.getHostPath()
	}

	return openWorkflowConfigForm(m, &values, nil)
}

func editWorkflowForm(m *model, workflow panel.Workflow) screen.Request {
	values := workflowFormValuesFrom(workflow.Config())

	return openWorkflowConfigForm(m, &values, &workflow)
}

// openWorkflowConfigForm puts up the form for a workflow's whole configuration, creating one
// when target is nil and saving over the workflow it names otherwise.
func openWorkflowConfigForm(m *model, values *workflowFormValues, target *panel.Workflow) screen.Request {
	title := "New workflow name"
	if target != nil {
		title = "Workflow name"
	}

	return form.Open{Form: workflowForm(workflowFormParams{
		NameTitle: title,
		Values:    values,
		Editing:   target != nil,
		Hooks:     m.getAvailableHooks,
		Submit: func(form.Inputs) screen.Request {
			if target == nil {
				return m.createWorkflowWrite(values.toConfig())
			}

			edited := values.toConfig().Workflow(target.ID)

			return m.write(
				"saving "+target.Name,
				fmt.Sprintf("Could not save workflow %q", target.Name),
				func(a api) error { return a.updateWorkflow(edited) },
			)
		},
	})}
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

func (m *model) archiveWorkflow(workflow panel.Workflow) screen.Request {
	return m.archivingWrite(archivingWriteParams{
		doing:   "archiving " + workflow.Name,
		summary: fmt.Sprintf("Could not archive workflow %q", workflow.Name),
		agents:  m.listing.AgentsOf(workflow.ID),
		awaits:  screen.Awaits{Gone: screen.RowsIn(screen.ListWorkflows, workflow.ID)},
		write: func(a api) error {
			return a.archiveWorkflow(workflow.ID)
		},
	})
}

// eventPromptEditables are the event prompts the panel's editor key can open.
func eventPromptEditables(values *workflowFormValues) []form.Editable {
	return xslice.Map(eventprompts.All(), func(event eventprompts.Event) form.Editable {
		return form.Editable{Key: eventPromptKey(event.Name), Value: values.eventPromptFor(event.Name)}
	})
}
