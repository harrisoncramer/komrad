package panel

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m model) View() string {
	switch m.screen {
	case screenForm:
		return m.form.View()
	case screenConfirm:
		return m.confirmView()
	case screenAction:
		return m.actionView()
	case screenList:
		return m.listView()
	default:
		return m.listView()
	}
}

func (m model) confirmView() string {
	yes, no := "  Yes  ", "  No  "
	if m.confirmYes {
		yes = cursorStyle.Render("▸ Yes ")
	} else {
		no = cursorStyle.Render("▸ No ")
	}

	return headerStyle.Render("destroy") + "\n\n  Kill session '" + m.agent.Session + "'?" +
		"\n\n  " + yes + "   " + no + "\n"
}

func (m model) actionView() string {
	var out strings.Builder
	out.WriteString(headerStyle.Render(m.agent.Session) + "\n\n")
	for i, action := range m.actions {
		out.WriteString(renderRow(action, i == m.actionIndex))
	}

	out.WriteString("\n" + dimStyle.Render("enter select · esc back"))
	return out.String()
}

func (m model) listView() string {
	var out strings.Builder
	out.WriteString(headerStyle.Render("Claude agents") + "\n\n")
	for i, r := range m.rows {
		out.WriteString(renderRow(rowLabel(r), i == m.cursor))
	}

	out.WriteString("\n" + dimStyle.Render("enter select · q quit"))
	return out.String()
}

func rowLabel(r row) string {
	if r.kind != rowAgent {
		return r.label
	}

	icon := lipgloss.NewStyle().Foreground(stateColor(r.agent.State)).Render(stateIcon(r.agent.State))
	label := icon + "  " + r.agent.Session
	if r.agent.Msg != "" {
		label += dimStyle.Render("  —  " + r.agent.Msg)
	}

	return label
}

func renderRow(label string, selected bool) string {
	cursor := "  "
	if selected {
		cursor = cursorStyle.Render("› ")
	}

	return cursor + label + "\n"
}
