package agents

import (
	"fmt"

	"komrad/internal/eventprompts"
	"komrad/internal/ui/api"
	"komrad/internal/ui/form"
	"komrad/internal/ui/panel"
	"komrad/internal/ui/screen"
	"komrad/lib/protoenum"
	"komrad/lib/xslice"
	eventv1 "komrad/proto/gen/event/v1"
	hookv1 "komrad/proto/gen/hook/v1"
)

func (a *Screen) newWorkflowForm(in screen.Inputs) screen.Request {
	values := workflowFormValues{
		EventPrompts: eventPromptsByEvent(xslice.Map(protoenum.Values[eventv1.Event](), func(event eventv1.Event) panel.EventPrompt {
			return panel.EventPrompt{Event: event, Prompt: eventprompts.DefaultPrompt(event)}
		})),
	}
	if len(in.Listing.Workflows) == 0 {
		values.Cwd = a.deps.Panes.GetHostPath()
	}

	return a.openWorkflowConfigForm(&values, nil)
}

func (a *Screen) editWorkflowForm(workflow panel.Workflow) screen.Request {
	values := workflowFormValuesFrom(workflow.Config())

	return a.openWorkflowConfigForm(&values, &workflow)
}

// openWorkflowConfigForm puts up the form for a workflow's whole configuration, creating one
// when target is nil and saving over the workflow it names otherwise.
func (a *Screen) openWorkflowConfigForm(values *workflowFormValues, target *panel.Workflow) screen.Request {
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
				return createWorkflowWrite(a.deps.Clients, values.toConfig())
			}

			edited := values.toConfig().Workflow(target.ID)

			return a.deps.Clients.Write(
				"saving "+target.Name,
				fmt.Sprintf("Could not save workflow %q", target.Name),
				func(clients api.Clients) error { return clients.UpdateWorkflow(edited) },
			)
		},
	})}
}

// getAvailableHooks is the hooks directory as the form offers it, a directory that is not there
// reading as an empty list rather than an error.
func (a *Screen) getAvailableHooks() []string {
	listed, err := a.deps.Clients.ListHooks()
	if err != nil {
		return nil
	}

	return xslice.Map(listed, func(hook *hookv1.Hook) string {
		return hook.GetName()
	})
}

func (a *Screen) archiveWorkflow(in screen.Inputs, workflow panel.Workflow) screen.Request {
	return a.deps.Clients.ArchivingWrite(api.ArchivingWriteParams{
		InFlightText:   "archiving " + workflow.Name,
		FailureSummary: fmt.Sprintf("Could not archive workflow %q", workflow.Name),
		Agents:         in.Listing.AgentsOf(workflow.ID),
		WaitFor:        screen.WaitFor{RowsGone: screen.RowsIn(screen.ListWorkflows, workflow.ID)},
		Write: func(clients api.Clients) error {
			return clients.ArchiveWorkflow(workflow.ID)
		},
	})
}

// eventPromptEditables are the event prompts the panel's editor key can open.
func eventPromptEditables(values *workflowFormValues) []form.Editable {
	return xslice.Map(protoenum.Values[eventv1.Event](), func(event eventv1.Event) form.Editable {
		return form.Editable{Key: eventPromptKey(event), Value: values.eventPromptFor(event)}
	})
}
