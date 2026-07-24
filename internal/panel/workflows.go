package panel

import (
	"fmt"
	"strings"

	"claude-agents/internal/appconfig"
	"claude-agents/internal/store"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// unassignLabel is the leading row shown when the picker is in move mode, so a
// chat can be pulled out of every workflow back to the root.
const unassignLabel = "(no workflow)"

// enterWorkflows opens the workflow picker, refreshing the list first. The
// caller sets moveTarget beforehand: nil to browse and scope into a workflow,
// or a chat to reassign it.
func (m model) enterWorkflows() (tea.Model, tea.Cmd) {
	m.reloadWorkflows()
	m.workflowCursor = 0
	m.screen = screenWorkflows

	return m, nil
}

// exitWorkflowScope leaves a scoped view and returns to the workflow picker,
// dropping the scope so the root chat listing resumes.
func (m model) exitWorkflowScope() (tea.Model, tea.Cmd) {
	m.workflow = nil
	m.moveTarget = nil
	m.reload()
	m.cursor = 0

	return m.enterWorkflows()
}

// updateWorkflows drives the workflow picker. In browse mode the actions mirror
// the chats view (open, new, rename, delete); in move mode the list gains a
// leading unassign row and selecting a row reassigns the chat.
func (m model) updateWorkflows(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	moving := m.moveTarget != nil

	switch {
	case bound(k, m.keys.Workflows.Quit):
		return m, m.hide()
	case bound(k, m.keys.Workflows.Back):
		if moving {
			m.moveTarget = nil
			m.screen = screenChats
			m.refreshPreview()
			return m, tick()
		}

		m.screen = screenList
		return m, nil
	case bound(k, m.keys.Nav.Up):
		if m.workflowCursor > 0 {
			m.workflowCursor--
		}
	case bound(k, m.keys.Nav.Down):
		if m.workflowCursor < len(m.workflowRows())-1 {
			m.workflowCursor++
		}
	case bound(k, m.keys.Workflows.Open):
		return m.selectWorkflow()
	case bound(k, m.keys.Workflows.New):
		if !moving {
			return m.newWorkflowForm()
		}
	case bound(k, m.keys.Workflows.Rename):
		if !moving && len(m.workflows) > 0 {
			return m.renameWorkflowForm()
		}
	case bound(k, m.keys.Workflows.Edit):
		if !moving && len(m.workflows) > 0 {
			return m.editWorkflowForm()
		}
	case bound(k, m.keys.Workflows.Delete):
		if !moving && len(m.workflows) > 0 {
			return m.deleteWorkflowConfirm()
		}
	}

	return m, nil
}

// selectWorkflow acts on the highlighted row: in move mode it reassigns the
// chat, otherwise it scopes the view into the chosen workflow.
func (m model) selectWorkflow() (tea.Model, tea.Cmd) {
	if m.moveTarget != nil {
		return m.applyMove()
	}

	if m.workflowCursor < 0 || m.workflowCursor >= len(m.workflows) {
		return m, nil
	}

	workflow := m.workflows[m.workflowCursor]
	m.workflow = &workflow
	m.reload()
	m.screen = screenList
	m.cursor = 0

	return m, nil
}

// applyMove reassigns the move target to the highlighted workflow, or unassigns
// it when the leading unassign row is selected, then returns to the chats view.
func (m model) applyMove() (tea.Model, tea.Cmd) {
	target := *m.moveTarget

	var workflowID *string
	if m.workflowCursor > 0 {
		index := m.workflowCursor - 1
		if index < len(m.workflows) {
			id := m.workflows[index].ID
			workflowID = &id
		}
	}

	m.manager.MoveChat(target, workflowID)
	m.moveTarget = nil
	m.reload()
	m.chatCursor = clampCursor(m.chatCursor, len(m.chats))

	// Moving a chat out of the current scope can empty the scoped list, whose
	// key handlers index m.chats directly, so fall back to the scoped root list
	// when nothing is left.
	if len(m.chats) == 0 {
		m.screen = screenList
		return m, nil
	}

	m.screen = screenChats
	m.refreshPreview()
	return m, tick()
}

// jumpToWorkflow scopes into the workflow the hovered chat belongs to and shows
// its chats with the same chat selected. A chat with no workflow is a no-op.
func (m model) jumpToWorkflow() (tea.Model, tea.Cmd) {
	if m.chatCursor < 0 || m.chatCursor >= len(m.chats) {
		return m, nil
	}

	agent := m.chats[m.chatCursor].agent
	if agent.WorkflowID == nil {
		return m, nil
	}

	workflow, err := m.manager.GetWorkflow(*agent.WorkflowID)
	if err != nil || workflow == nil {
		return m, nil
	}

	m.workflow = workflow
	m.reload()
	m.chatCursor = clampCursor(indexOfChat(m.chats, agent.Name), len(m.chats))
	if len(m.chats) == 0 {
		m.screen = screenList
		return m, nil
	}

	m.screen = screenChats
	m.refreshPreview()
	return m, tick()
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
// leaves everything untouched. All three return to the picker.
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
		m.screen = screenWorkflows
		return m, nil
	case huh.StateAborted:
		m.workflowDeleteTarget = nil
		m.screen = screenWorkflows
		return m, nil
	default:
		return m, cmd
	}
}

// updateWorkflowForm drives the create/rename form, submitting on the configured
// submit key or on completion and aborting back to the picker.
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
		m.screen = screenWorkflows
		return m, nil
	default:
		return m, cmd
	}
}

// submitWorkflowForm applies the form: renaming the target workflow, editing an
// existing one's config, or creating a new one, then returns to the picker with
// the list refreshed.
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
	m.screen = screenWorkflows
	return m, nil
}

// workflowRows returns the labels shown in the picker: the workflow names, with
// a leading unassign row in move mode.
func (m model) workflowRows() []string {
	rows := make([]string, 0, len(m.workflows)+1)
	if m.moveTarget != nil {
		rows = append(rows, unassignLabel)
	}
	for _, workflow := range m.workflows {
		rows = append(rows, workflow.Name)
	}

	return rows
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

func (m model) workflowsView() string {
	var out strings.Builder

	header := "Workflows"
	if m.moveTarget != nil {
		header = fmt.Sprintf("Move %q to", m.moveTarget.Name)
	}
	out.WriteString(headerStyle.Render(header) + "\n\n")

	rows := m.workflowRows()
	if len(rows) == 0 {
		out.WriteString(dimStyle.Render("no workflows yet") + "\n")
	}
	for i, label := range rows {
		cursor := "  "
		text := label
		if i == m.workflowCursor {
			cursor = cursorStyle.Render("› ")
			text = cursorStyle.Render(label)
		}
		out.WriteString(cursor + text + "\n")
	}

	out.WriteString("\n" + m.workflowHints())
	return out.String()
}

// workflowHints renders the picker's key hints, which differ between move mode
// and browse mode.
func (m model) workflowHints() string {
	if m.moveTarget != nil {
		return dimStyle.Render(
			hintKeys(m.keys.Workflows.Open) + " move · " +
				hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down) + " select · " +
				hintKeys(m.keys.Workflows.Back) + " cancel")
	}

	return dimStyle.Render(
		hintKeys(m.keys.Workflows.Open) + " open · " +
			hintKeys(m.keys.Workflows.New) + " new · " +
			hintKeys(m.keys.Workflows.Rename) + " rename · " +
			hintKeys(m.keys.Workflows.Edit) + " edit · " +
			hintKeys(m.keys.Workflows.Delete) + " delete · " +
			hintKeys(m.keys.Workflows.Back) + " back")
}
