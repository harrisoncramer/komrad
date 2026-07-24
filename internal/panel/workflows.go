package panel

import (
	"fmt"

	"claude-agents/internal/appconfig"
	"claude-agents/internal/store"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// updateHoveredWorkflow handles keys while the chats-tree cursor is on a workflow
// header. The workflow bindings act on that folder's workflow, while nav still
// moves the tree cursor and any form it opens returns to the chats view. wfIdx is
// the header's index in m.workflows.
func (m model) updateHoveredWorkflow(key tea.KeyMsg, wfIdx int) (tea.Model, tea.Cmd) {
	k := key.String()
	m.workflowCursor = wfIdx

	switch {
	case bound(k, m.keys.Chats.Quit):
		return m, m.hide()
	case bound(k, m.keys.Chats.Back):
		return m, m.hide()
	case bound(k, m.keys.Nav.Up):
		if m.chatCursor > 0 {
			m.chatCursor--
			m.stageSelection()
		}
	case bound(k, m.keys.Nav.Down):
		if m.chatCursor < len(m.chatRows())-1 {
			m.chatCursor++
			m.stageSelection()
		}
	case bound(k, m.keys.Chats.New):
		workflow := m.workflows[wfIdx]
		return m.openNewSession(&workflow)
	case bound(k, m.keys.Workflows.Rename):
		return m.renameWorkflowForm()
	case bound(k, m.keys.Workflows.Edit):
		return m.editWorkflowForm()
	case bound(k, m.keys.Workflows.Delete):
		return m.deleteWorkflowConfirm()
	}

	return m, nil
}

// returnFromWorkflow sends the user back to the chats view after a workflow form
// or dialog closes, restaging for the current selection.
func (m model) returnFromWorkflow() (tea.Model, tea.Cmd) {
	m.screen = screenChats
	m.chatCursor = clampCursor(m.chatCursor, len(m.chatRows()))
	m.stageSelection()

	return m, nil
}

// newWorkflowForm opens the form to create a workflow: a name plus the default
// cwd and flags that seed new chats created inside it.
func (m model) newWorkflowForm() (tea.Model, tea.Cmd) {
	m.workflowRenameTarget = nil
	m.workflowEditTarget = nil
	m.form = workflowConfigForm("New workflow name", store.WorkflowConfig{}, m.keys.Form)
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
	if fresh, err := m.manager.GetWorkflow(workflow.ID); err == nil && fresh != nil {
		workflow = *fresh
	}
	m.workflowEditTarget = &workflow
	m.workflowRenameTarget = nil
	m.form = workflowConfigForm("Workflow name", store.WorkflowConfig{
		Name:    workflow.Name,
		Context: workflow.Context,
		Cwd:     workflow.Cwd,
		Flags:   workflow.Flags,
	}, m.keys.Form)
	m.screen = screenWorkflowForm

	return m, m.form.Init()
}

// deleteWorkflowConfirm opens the delete prompt for the highlighted workflow,
// asking whether its chats should be removed with it.
func (m model) deleteWorkflowConfirm() (tea.Model, tea.Cmd) {
	workflow := m.workflows[m.workflowCursor]
	m.workflowDeleteTarget = &workflow
	m.form = deleteWorkflowForm(workflow.Name, m.keys.Form)
	m.screen = screenWorkflowDelete

	return m, m.form.Init()
}

// updateWorkflowDelete drives the delete prompt. Confirming purges the workflow
// with its chats, declining orphans the chats back to unassigned, and aborting
// leaves everything untouched. All three return to the chats view.
func (m model) updateWorkflowDelete(msg tea.Msg) (tea.Model, tea.Cmd) {
	form, cmd := m.form.Update(msg)
	if updated, ok := form.(*huh.Form); ok {
		m.form = updated
	}

	switch m.form.State {
	case huh.StateCompleted:
		if m.workflowDeleteTarget != nil {
			if m.form.GetBool("purge") {
				m.manager.DeleteWorkflowWithChats(*m.workflowDeleteTarget)
			} else {
				m.manager.DeleteWorkflow(*m.workflowDeleteTarget)
			}
		}
		m.workflowDeleteTarget = nil
		m.reloadWorkflows()
		return m.returnFromWorkflow()
	case huh.StateAborted:
		m.workflowDeleteTarget = nil
		return m.returnFromWorkflow()
	default:
		return m, cmd
	}
}

// updateWorkflowForm drives the create/rename form, submitting on the configured
// submit key or on completion and aborting back to the chats view.
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
		return m.returnFromWorkflow()
	default:
		return m, cmd
	}
}

// submitWorkflowForm applies the form: renaming the target workflow, editing an
// existing one's config, or creating a new one, then returns to the chats view
// with the list refreshed.
func (m model) submitWorkflowForm() (tea.Model, tea.Cmd) {
	name := m.form.GetString("name")
	switch {
	case m.workflowRenameTarget != nil:
		m.manager.RenameWorkflow(*m.workflowRenameTarget, name)
	case m.workflowEditTarget != nil:
		m.manager.UpdateWorkflow(*m.workflowEditTarget, store.WorkflowConfig{
			Name:    name,
			Context: m.form.GetString("context"),
			Cwd:     m.form.GetString("dir"),
			Flags:   m.form.GetString("flags"),
		})
	default:
		_, _ = m.manager.NewWorkflow(store.WorkflowConfig{
			Name:    name,
			Context: m.form.GetString("context"),
			Cwd:     m.form.GetString("dir"),
			Flags:   m.form.GetString("flags"),
		})
	}

	m.workflowRenameTarget = nil
	m.workflowEditTarget = nil
	m.reloadWorkflows()
	return m.returnFromWorkflow()
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
// new-chat form for chats created inside the workflow. Every field prefills, so
// it serves both creating a workflow (blank) and editing one (its current
// values).
func workflowConfigForm(nameTitle string, cfg store.WorkflowConfig, keys appconfig.FormKeys) *huh.Form {
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

// deleteWorkflowForm asks whether a workflow's chats should be deleted with it.
// Confirming purges them, declining orphans them back to unassigned, and the
// abort key cancels the delete entirely.
func deleteWorkflowForm(name string, keys appconfig.FormKeys) *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Key("purge").
				Title(fmt.Sprintf("Delete workflow %q — also delete its chats?", name)).
				Description("No keeps the chats and unassigns them.").
				Affirmative("Delete chats too").
				Negative("Keep chats"),
		),
	).WithShowHelp(true).WithKeyMap(formKeyMap(keys))
}
