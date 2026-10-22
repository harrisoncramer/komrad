package ui

import (
	"fmt"

	"komrad/internal/eventprompts"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/xslice"
	hookv1 "komrad/proto/gen/hook/v1"
)

func (a *agentsScreen) newWorkflowForm(m *model) screen.Request {
	values := workflowFormValues{
		EventPrompts: eventPromptsByEvent(xslice.Map(eventprompts.All(), func(event eventprompts.Event) panel.EventPrompt {
			return panel.EventPrompt{Event: event.Name, Prompt: event.DefaultPrompt}
		})),
	}
	if len(m.listing.Workflows) == 0 {
		values.Cwd = a.deps.panes.GetHostPath()
	}

	return a.openWorkflowConfigForm(&values, nil)
}

func (a *agentsScreen) editWorkflowForm(workflow panel.Workflow) screen.Request {
	values := workflowFormValuesFrom(workflow.Config())

	return a.openWorkflowConfigForm(&values, &workflow)
}

// openWorkflowConfigForm puts up the form for a workflow's whole configuration, creating one
// when target is nil and saving over the workflow it names otherwise.
func (a *agentsScreen) openWorkflowConfigForm(values *workflowFormValues, target *panel.Workflow) screen.Request {
	title := "New workflow name"
	if target != nil {
		title = "Workflow name"
	}

	return form.Open{Form: workflowForm(workflowFormParams{
		NameTitle: title,
		Values:    values,
		Editing:   target != nil,
		Hooks:     a.getAvailableHooks,
		Submit: func(form.Inputs) screen.Request {
			if target == nil {
				return createWorkflowWrite(a.deps.clients, values.toConfig())
			}

			edited := values.toConfig().Workflow(target.ID)

			return a.deps.clients.Write(
				"saving "+target.Name,
				fmt.Sprintf("Could not save workflow %q", target.Name),
				func(clients api.Clients) error { return clients.UpdateWorkflow(edited) },
			)
		},
	})}
}

// getAvailableHooks is the hooks directory as the form offers it, a directory that is not there
// reading as an empty list rather than an error.
func (a *agentsScreen) getAvailableHooks() []string {
	listed, err := a.deps.clients.ListHooks()
	if err != nil {
		return nil
	}

	return xslice.Map(listed, func(hook *hookv1.Hook) string {
		return hook.GetName()
	})
}

func (a *agentsScreen) archiveWorkflow(m *model, workflow panel.Workflow) screen.Request {
	return a.deps.clients.ArchivingWrite(api.ArchivingWriteParams{
		Doing:   "archiving " + workflow.Name,
		Summary: fmt.Sprintf("Could not archive workflow %q", workflow.Name),
		Agents:  m.listing.AgentsOf(workflow.ID),
		Awaits:  screen.Awaits{Gone: screen.RowsIn(screen.ListWorkflows, workflow.ID)},
		Write: func(clients api.Clients) error {
			return clients.ArchiveWorkflow(workflow.ID)
		},
	})
}

// eventPromptEditables are the event prompts the panel's editor key can open.
func eventPromptEditables(values *workflowFormValues) []form.Editable {
	return xslice.Map(eventprompts.All(), func(event eventprompts.Event) form.Editable {
		return form.Editable{Key: eventPromptKey(event.Name), Value: values.eventPromptFor(event.Name)}
	})
}
