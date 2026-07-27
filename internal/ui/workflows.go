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
	m.form = workflowConfigForm("New workflow name", workflowstore.Config{}, m.keys.Form)
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

// renameWorkflowForm opens the single-input form to rename the highlighted
// workflow.
func (m model) renameWorkflowForm() (tea.Model, tea.Cmd) {
	workflow := m.workflows[m.workflowCursor]
	m.workflowRenameTarget = &workflow
	m.workflowEditTarget = nil
	m.form = workflowForm(fmt.Sprintf("Rename %q to", workflow.Name), m.keys.Form)
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
	m.form = workflowConfigForm("Workflow name", workflowstore.Config{
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
	m.form = deleteWorkflowForm(workflow.Name, m.keys.Form)
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
		if m.workflowDeleteTarget != nil && m.form.GetBool("confirm") {
			m.report(
				fmt.Sprintf("Could not delete workflow %q", m.workflowDeleteTarget.Name),
				m.deleteWorkflow(m.workflowDeleteTarget.ID),
			)
		}
		m.workflowDeleteTarget = nil
		m.reloadWorkflows()
		return m.returnToAgents()
	case huh.StateAborted:
		m.workflowDeleteTarget = nil
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
		return m.returnToAgents()
	default:
		return m, cmd
	}
}

// submitWorkflowForm applies the form, renaming the target workflow, editing an
// existing one's config, or creating a new one, then returns to the agents view
// with the list refreshed.
func (m model) submitWorkflowForm() (tea.Model, tea.Cmd) {
	name := m.form.GetString("name")
	switch {
	case m.workflowRenameTarget != nil:
		// A rename is an update carrying the fields it is not changing, since the
		// service replaces all of them.
		target := *m.workflowRenameTarget
		if trimmed := strings.TrimSpace(name); trimmed != "" && trimmed != target.Name {
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
				Name:    name,
				Context: m.form.GetString("context"),
				Cwd:     m.form.GetString("dir"),
				Flags:   m.form.GetString("flags"),
			}),
		)
	default:
		m.report("Could not create the workflow", m.createWorkflow(workflowstore.Config{
			Name:    name,
			Context: m.form.GetString("context"),
			Cwd:     m.form.GetString("dir"),
			Flags:   m.form.GetString("flags"),
		}))
	}

	m.workflowRenameTarget = nil
	m.workflowEditTarget = nil
	m.reloadWorkflows()
	return m.returnToAgents()
}

func workflowForm(title string, keys appconfig.FormKeys) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(title),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys))
}

// workflowConfigForm collects a workflow's name, its free-text context, and its
// default config. The dir and flags become the blueprint prefilled into the
// new-agent form for agents created inside the workflow. Every field prefills, so
// it serves both creating a workflow with blank values and editing one with its
// current values.
func workflowConfigForm(nameTitle string, cfg workflowstore.Config, keys appconfig.FormKeys) *huh.Form {
	name, workflowContext, dir, flags := cfg.Name, cfg.Context, cfg.Cwd, cfg.Flags
	return huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Key("name").Title(nameTitle).Value(&name),
			huh.NewText().Key("context").Title("Context").Description("shared notes for this workflow; ctrl+e opens $EDITOR").Value(&workflowContext),
			huh.NewInput().Key("dir").Title("Default directory").Description("blank = current").Value(&dir),
			huh.NewInput().Key("flags").Title("Default claude flags").Placeholder("--model opus").Value(&flags),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys))
}

// deleteWorkflowForm confirms a delete. The agents inside go with it, since
// every agent belongs to a workflow and there is nowhere to leave one.
func deleteWorkflowForm(name string, keys appconfig.FormKeys) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Key("confirm").
				Title(fmt.Sprintf("Delete workflow %q and its agents?", name)).
				Description("Their transcripts are kept and stay searchable.").
				Affirmative("Delete").
				Negative("Cancel"),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys))
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
