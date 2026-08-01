package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The key legend under every list. Each screen names its own bindings, and a toggle names
// the direction the row would go, so a legend says what the key would do to what is under
// the cursor.

// hint is one line of the key legend under a list, a binding and what it does.
type hint struct{ key, desc string }

// renderHints lays the legend out as two columns, the keys padded to the widest so the
// descriptions line up.
func renderHints(hints []hint) string {
	keyWidth := 0
	for _, h := range hints {
		if w := lipgloss.Width(h.key); w > keyWidth {
			keyWidth = w
		}
	}
	keyStyle := lipgloss.NewStyle().Width(keyWidth + 1)

	var b strings.Builder
	for _, h := range hints {
		b.WriteString(dimStyle.Render(keyStyle.Render(h.key) + h.desc))
		b.WriteString("\n")
	}

	return b.String()
}

// agentHints lists the agents screen's bindings, one per line with an aligned key column.
func (m model) agentHints() string {
	// The key that draws the hidden agents toggles, so the legend names the direction it
	// would go.
	showHiddenDesc := "show hidden"
	if m.tree.showHidden {
		showHiddenDesc = "put hidden away"
	}

	hints := append(m.rowHints(), m.screenHints()...)
	hints = append(hints,
		hint{hintKeys(m.keys.Agents.ShowHidden), showHiddenDesc},
		hint{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		hint{hintKeys(m.keys.Agents.Back), "back"},
	)

	return renderHints(hints)
}

// rowHints are the bindings that act on the row under the cursor, a workflow header or an
// agent. The info, mark, and hide keys toggle, so their lines name what that row would do.
func (m model) rowHints() []hint {
	if _, hoveringWorkflow := m.hoveredWorkflow(); hoveringWorkflow {
		return []hint{
			{hintKeys(m.keys.Agents.New), "new agent"},
			{hintKeys(m.keys.Workflows.New), "new workflow"},
			{hintKeys(m.keys.Workflows.Rename), "rename"},
			{hintKeys(m.keys.Workflows.Edit), "edit"},
			{hintKeys(m.keys.Workflows.Delete), "delete"},
		}
	}

	infoDesc := "info"
	if m.tree.infoPinned {
		infoDesc = "hide info"
	}

	markDesc, hideDesc := "mark", "hide"
	if item, ok := m.selectedAgent(); ok {
		if next := nextMark(item.agent.Mark, m.marks); next != nil {
			markDesc = "mark " + *next
		} else {
			markDesc = "unmark"
		}
		if item.agent.Hidden {
			hideDesc = "unhide"
		}
	}

	return []hint{
		{hintKeys(m.keys.Agents.Open), "open"},
		{hintKeys(m.keys.Agents.Editor), "editor"},
		{hintKeys(m.keys.Agents.Info), infoDesc},
		{hintKeys(m.keys.Actions.Open), "actions"},
		{hintKeys(m.keys.Agents.Mark), markDesc},
		{hintKeys(m.keys.Agents.Hide), hideDesc},
		{hintKeys(m.keys.Agents.New), "new agent"},
		{hintKeys(m.keys.Workflows.New), "new workflow"},
		{hintKeys(m.keys.Agents.Rename), "rename"},
		{hintKeys(m.keys.Agents.Archive), "archive"},
	}
}

// screenHints are the screens reachable from anywhere in the tree, appended to whichever
// legend is showing, since none of them act on the row under the cursor.
func (m model) screenHints() []hint {
	return []hint{
		{hintKeys(m.keys.Agents.Search), "search"},
		{hintKeys(m.keys.Schedules.Open), "schedules"},
		{hintKeys(m.keys.Archive.Open), "archived agents"},
		{hintKeys(m.keys.Agents.Logs), m.logsDesc()},
	}
}

// scheduleHints renders the schedules screen's key hints, in the two-column shape the
// agents view uses.
func (m model) scheduleHints() string {
	toggle := "pause"
	if item, ok := m.selectedSchedule(); ok && !item.schedule.Enabled {
		toggle = "resume"
	}

	return renderHints([]hint{
		{hintKeys(m.keys.Schedules.New), "new schedule"},
		{hintKeys(m.keys.Schedules.Edit), "edit"},
		{hintKeys(m.keys.Schedules.Toggle), toggle},
		{hintKeys(m.keys.Schedules.Delete), "delete"},
		{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		{hintKeys(m.keys.Schedules.Back), "back"},
	})
}

// archiveHints lists the archive screen's bindings.
func (m model) archiveHints() string {
	return renderHints([]hint{
		{hintKeys(m.keys.Archive.Restore), "restore"},
		{hintKeys(m.keys.Archive.Delete), "delete for good"},
		{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		{hintKeys(m.keys.Archive.Back), "back"},
	})
}

// actionHints lists the actions screen's bindings. The run line names the chmod a file
// without the executable bit wants, since a dim row cannot say why it is dim.
func (m model) actionHints() string {
	runDesc := "run"
	if action, ok := m.selectedAction(); ok && !action.executable {
		runDesc = "chmod +x to run"
	}

	return renderHints([]hint{
		{hintKeys(m.keys.Actions.Run), runDesc},
		{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		{hintKeys(m.keys.Actions.Back), "back"},
	})
}

// searchHints lists the search screen's fixed key hints.
func (m model) searchHints() string {
	hints := []struct{ key, desc string }{
		{"enter", "open"},
		{hintKeys(m.keys.Nav.Up) + "/" + hintKeys(m.keys.Nav.Down), "move"},
		{"esc", "back"},
	}

	keyWidth := 0
	for _, h := range hints {
		if w := lipgloss.Width(h.key); w > keyWidth {
			keyWidth = w
		}
	}
	keyStyle := lipgloss.NewStyle().Width(keyWidth + 1)

	var b strings.Builder
	for _, h := range hints {
		b.WriteString(dimStyle.Render(keyStyle.Render(h.key) + h.desc))
		b.WriteString("\n")
	}

	return b.String()
}

// hintKeys renders a binding's keys for the help text, joining them with "/" and
// shortening "ctrl+x" to "C-x". The space bar is named rather than printed, since a
// literal space reads as a binding that failed to load.
func hintKeys(keys []string) string {
	out := make([]string, len(keys))
	for i, k := range keys {
		if k == " " {
			out[i] = "space"
			continue
		}

		out[i] = strings.Replace(k, "ctrl+", "C-", 1)
	}

	return strings.Join(out, "/")
}
