package panel

import (
	"strings"
)

func (m model) View() string {
	switch m.screen {
	case screenForm:
		return m.form.View()
	case screenChats:
		return m.chatsView()
	case screenList:
		return m.listView()
	default:
		return m.listView()
	}
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
