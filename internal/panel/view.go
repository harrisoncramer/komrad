package panel

import (
	"strings"
)

func (m model) View() string {
	switch m.screen {
	case screenForm:
		return m.form.View()
	case screenConfirm:
		return m.confirmView()
	case screenChats:
		return m.chatsView()
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

	return headerStyle.Render("kill") + "\n\n  Kill session '" + m.agent.Name + "'?" +
		"\n\n  " + yes + "   " + no + "\n"
}

func (m model) listView() string {
	var out strings.Builder
	out.WriteString(headerStyle.Render("Claude agents") + "\n\n")
	for i, r := range m.rows {
		out.WriteString(renderRow(r.label, i == m.cursor))
	}

	out.WriteString("\n" + dimStyle.Render("enter select · q quit"))
	return out.String()
}

func renderRow(label string, selected bool) string {
	cursor := "  "
	if selected {
		cursor = cursorStyle.Render("› ")
	}

	return cursor + label + "\n"
}
