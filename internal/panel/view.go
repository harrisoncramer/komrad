package panel

import (
	"strings"
)

func (m model) View() string {
	switch m.screen {
	case screenForm, screenRename, screenWorkflowForm, screenWorkflowDelete:
		return m.form.View()
	case screenChats:
		return m.chatsView()
	case screenSearch:
		return m.searchView()
	case screenWorkflows:
		return m.workflowsView()
	case screenList:
		return m.listView()
	default:
		return m.listView()
	}
}

func (m model) listView() string {
	var out strings.Builder

	title := "Claude agents"
	if m.workflow != nil {
		title = "Workflow: " + m.workflow.Name
	}
	out.WriteString(headerStyle.Render(title) + "\n\n")

	for i, r := range m.rows {
		out.WriteString(renderRow(r.label, i == m.cursor))
	}

	hints := hintKeys(m.keys.List.New) + " new · " +
		hintKeys(m.keys.List.Chats) + " chats · "
	if m.workflow == nil {
		hints += hintKeys(m.keys.List.Workflows) + " workflows · "
	}
	hints += hintKeys(m.keys.List.Search) + " search · " +
		hintKeys(m.keys.List.Select) + " select · "
	if m.workflow != nil {
		hints += hintKeys(m.keys.List.Back) + " back · "
	}
	hints += hintKeys(m.keys.List.Quit) + " quit"

	out.WriteString("\n" + dimStyle.Render(hints))
	return out.String()
}

func renderRow(label string, selected bool) string {
	cursor := "  "
	if selected {
		cursor = cursorStyle.Render("› ")
	}

	return cursor + label + "\n"
}
