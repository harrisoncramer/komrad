package ui

import (
	"context"
	"fmt"
	"strings"

	"claude-agents/internal/appconfig"
	workflowv1 "claude-agents/proto/gen/workflow/v1"

	"connectrpc.com/connect"

	workflowstore "claude-agents/internal/store/workflow"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// updateHoveredWorkflow handles keys while the agents-tree cursor is on a workflow
// header. The workflow bindings act on that folder's workflow, while nav still
// moves the tree cursor and any form it opens returns to the agents view. wfIdx is
// the header's index in m.workflows.
func (m model) updateHoveredWorkflow(key tea.KeyMsg, wfIdx int) (tea.Model, tea.Cmd) {
	k := key.String()
	m.workflowCursor = wfIdx

	switch {
	case bound(k, m.keys.Agents.Quit):
		return m, m.hide()
	case bound(k, m.keys.Agents.Back):
		return m, m.hide()
	case bound(k, m.keys.Nav.Up):
		if m.agentCursor > 0 {
			m.agentCursor--
			m.stageSelection()
		}
	case bound(k, m.keys.Nav.Down):
		if m.agentCursor < len(m.agentRows())-1 {
			m.agentCursor++
			m.stageSelection()
		}
	case bound(k, m.keys.Agents.New):
		workflow := m.workflows[wfIdx]
		return m.openNewAgent(&workflow)
	case bound(k, m.keys.Workflows.Rename):
		return m.renameWorkflowForm()
	case bound(k, m.keys.Workflows.Edit):
		return m.editWorkflowForm()
	case bound(k, m.keys.Workflows.Delete):
		return m.deleteWorkflowConfirm()
	}

	return m, nil
}

// returnToAgents sends the user back to the agents view after a form, a dialog, or
// another screen closes, restaging for the current selection.
func (m model) returnToAgents() (tea.Model, tea.Cmd) {
	m.screen = screenAgents
	m.agentCursor = clampCursor(m.agentCursor, len(m.agentRows()))
	m.stageSelection()

	return m, nil
}

// newWorkflowForm opens the form to create a workflow with a name plus the
// default cwd and flags that seed new agents created inside it.
func (m model) newWorkflowForm() (tea.Model, tea.Cmd) {
	m.workflowRenameTarget = nil
	m.workflowEditTarget = nil
	m.form, m.workflowValues = workflowConfigForm("New workflow name", workflowstore.Config{}, m.keys.Form)
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

// renameWorkflowForm opens the single-input form to rename the highlighted
// workflow.
func (m model) renameWorkflowForm() (tea.Model, tea.Cmd) {
	workflow := m.workflows[m.workflowCursor]
	m.workflowRenameTarget = &workflow
	m.workflowEditTarget = nil
	m.form, m.workflowValues = workflowForm(fmt.Sprintf("Rename %q to", workflow.Name), m.keys.Form)
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

// editWorkflowForm reopens the create form for the highlighted workflow,
// prefilled with its current name, context, and default config, so its
// properties can be patched in place. It refetches the workflow so the form
// reflects the latest stored values, falling back to the listed row if the
// fetch fails.
func (m model) editWorkflowForm() (tea.Model, tea.Cmd) {
	workflow := m.workflows[m.workflowCursor]
	if fresh, err := m.getWorkflow(workflow.ID); err == nil && fresh != nil {
		workflow = *fresh
	}
	m.workflowEditTarget = &workflow
	m.workflowRenameTarget = nil
	m.form, m.workflowValues = workflowConfigForm("Workflow name", workflowstore.Config{
		Name:    workflow.Name,
		Context: workflow.Context,
		Cwd:     workflow.Cwd,
		Flags:   workflow.Flags,
	}, m.keys.Form)
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

// deleteWorkflowConfirm opens the delete prompt for the highlighted workflow,
// asking whether its agents should be removed with it.
func (m model) deleteWorkflowConfirm() (tea.Model, tea.Cmd) {
	workflow := m.workflows[m.workflowCursor]
	m.workflowDeleteTarget = &workflow
	m.form, m.confirmed = deleteWorkflowForm(workflow.Name, m.keys.Form)
	m.screen = screenWorkflowDelete

	return m, m.form.Init()
}

// updateWorkflowDelete drives the delete prompt. Confirming purges the workflow
// with its agents, declining orphans the agents back to unassigned, and aborting
// leaves everything untouched. All three return to the agents view.
func (m model) updateWorkflowDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		if m.workflowDeleteTarget != nil && m.wasConfirmed() {
			m.report(
				fmt.Sprintf("Could not delete workflow %q", m.workflowDeleteTarget.Name),
				m.deleteWorkflow(m.workflowDeleteTarget.ID),
			)
		}
		m.workflowDeleteTarget = nil
		m.confirmed = nil
		m.reloadWorkflows()
		return m.returnToAgents()
	case huh.StateAborted:
		m.workflowDeleteTarget = nil
		m.confirmed = nil
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
		m.workflowRenameTarget = nil
		m.workflowEditTarget = nil
		m.workflowValues = nil
		return m.returnToAgents()
	default:
		return m, cmd
	}
}

// submitWorkflowForm applies the form, renaming the target workflow, editing an
// existing one's config, or creating a new one, then returns to the agents view
// with the list refreshed.
func (m model) submitWorkflowForm() (tea.Model, tea.Cmd) {
	// The form writes into values the panel handed it, so a submit without them is a
	// submit for a form that was never opened.
	if m.workflowValues == nil {
		return m.returnToAgents()
	}

	values := *m.workflowValues
	switch {
	case m.workflowRenameTarget != nil:
		// A rename is an update carrying the fields it is not changing, since the
		// service replaces all of them.
		target := *m.workflowRenameTarget
		if trimmed := strings.TrimSpace(values.Name); trimmed != "" && trimmed != target.Name {
			target.Name = trimmed
			m.report(
				fmt.Sprintf("Could not rename workflow %q", m.workflowRenameTarget.Name),
				m.updateWorkflow(target),
			)
		}
	case m.workflowEditTarget != nil:
		m.report(
			fmt.Sprintf("Could not save workflow %q", m.workflowEditTarget.Name),
			m.updateWorkflow(workflowstore.Workflow{
				ID:      m.workflowEditTarget.ID,
				Name:    values.Name,
				Context: values.Context,
				Cwd:     values.Cwd,
				Flags:   values.Flags,
			}),
		)
	default:
		m.report("Could not create the workflow", m.createWorkflow(values))
	}

	m.workflowRenameTarget = nil
	m.workflowEditTarget = nil
	m.workflowValues = nil
	m.reloadWorkflows()
	return m.returnToAgents()
}

// workflowForm asks for a name alone, which is what a rename is. It returns the
// config its input writes into, the same shape the fuller form binds, so a submit
// reads either through one path.
func workflowForm(title string, keys appconfig.FormKeys) (*huh.Form, *workflowstore.Config) {
	var values workflowstore.Config

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(title).Value(&values.Name),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys))

	return form, &values
}

// workflowConfigForm collects a workflow's name, its free-text context, and its
// default config, and returns the config its fields write into. The dir and flags
// become the blueprint prefilled into the new-agent form for agents created inside the
// workflow. Every field prefills, so it serves both creating a workflow with blank
// values and editing one with its current values.
func workflowConfigForm(nameTitle string, cfg workflowstore.Config, keys appconfig.FormKeys) (*huh.Form, *workflowstore.Config) {
	values := cfg

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(nameTitle).Value(&values.Name),
			huh.NewText().Key("context").Title("Context").Description("shared notes for this workflow; ctrl+e opens $EDITOR").Value(&values.Context),
			huh.NewInput().Key("dir").Title("Default directory").Description("blank = current").Value(&values.Cwd),
			huh.NewInput().Key("flags").Title("Default claude flags").Placeholder("--model opus").Value(&values.Flags),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys))

	return form, &values
}

// deleteWorkflowForm confirms a delete. The agents inside go with it, since
// every agent belongs to a workflow and there is nowhere to leave one.
func deleteWorkflowForm(name string, keys appconfig.FormKeys) (*huh.Form, *bool) {
	var confirmed bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Key("confirm").
				Title(fmt.Sprintf("Delete workflow %q and its agents?", name)).
				Description("Their transcripts are kept and stay searchable.").
				Affirmative("Delete").
				Negative("Cancel").
				Value(&confirmed),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys))

	return form, &confirmed
}

// The workflow screens act through the service like everything else. Each call
// carries its own short deadline so a wedged server costs a keypress rather than
// the panel.

// getWorkflow refetches one workflow, so an edit form opens on what is stored
// rather than on the row the listing happened to carry.
func (m model) getWorkflow(id string) (*workflowstore.Workflow, error) {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	resp, err := m.api.workflows.GetWorkflow(ctx, connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: id}))
	if err != nil {
		return nil, fmt.Errorf("failed to get workflow %q: %w", id, err)
	}

	w := resp.Msg.GetWorkflow()

	return &workflowstore.Workflow{
		ID:      w.GetId(),
		Name:    w.GetName(),
		Context: w.GetContext(),
		Cwd:     w.GetCwd(),
		Flags:   w.GetFlags(),
	}, nil
}

// createWorkflow adds a workflow, defaulting a blank name so the list always has
// a label to render.
func (m model) createWorkflow(cfg workflowstore.Config) error {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = "workflow"
	}

	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := m.api.workflows.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:    name,
		Context: cfg.Context,
		Cwd:     cfg.Cwd,
		Flags:   cfg.Flags,
	}))

	return err
}

// updateWorkflow writes every field back, which is what the service expects. It
// replaces rather than patches, so the caller sends the whole workflow.
func (m model) updateWorkflow(w workflowstore.Workflow) error {
	name := strings.TrimSpace(w.Name)
	if name == "" {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := m.api.workflows.UpdateWorkflow(ctx, connect.NewRequest(&workflowv1.UpdateWorkflowRequest{
		Id:      w.ID,
		Name:    name,
		Context: w.Context,
		Cwd:     w.Cwd,
		Flags:   w.Flags,
	}))

	return err
}

// deleteWorkflow removes a workflow, archiving the agents inside it alongside
// it. The service refuses while any schedule still launches into it.
func (m model) deleteWorkflow(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), actionTimeout)
	defer cancel()

	_, err := m.api.workflows.DeleteWorkflow(ctx, connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: id}))

	return err
}
