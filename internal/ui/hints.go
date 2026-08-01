package ui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

// The key legend under every list.
type legend struct {
	short []key.Binding
	full  []key.Binding
}

// ShortHelp is the collapsed legend, the one line.
func (l legend) ShortHelp() []key.Binding {
	return l.short
}

// FullHelp is the expanded legend as a single column, so the bindings run down the pane
// one per line.
func (l legend) FullHelp() [][]key.Binding {
	return [][]key.Binding{l.full}
}

// renderHints draws a screen's legend, ending in the newline every block a screen builds
// ends in so the height budget counts it like the rest of the views.
func (m model) renderHints(l legend) string {
	view := help.New()
	view.Styles = helpStyles()
	view.Width = m.width
	view.ShowAll = !m.hintsCollapsed

	return trimLines(view.View(l)) + "\n"
}

// trimLines takes the padding off the end of every line of a legend.
func trimLines(legend string) string {
	lines := strings.Split(legend, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}

	return strings.Join(lines, "\n")
}

// hint is one line of the key legend, a configured binding paired with what it does.
func hint(keys []string, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(hintKeys(keys), desc))
}

// moveHint is the two nav bindings as one line, since neither direction is worth a line of
// its own.
func (m model) moveHint() key.Binding {
	keys := slices.Concat(m.keys.Nav.Up, m.keys.Nav.Down)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(hintKeys(m.keys.Nav.Up)+"/"+hintKeys(m.keys.Nav.Down), "move"),
	)
}

// helpHint is the binding that draws the legend itself, which ends every screen's legend in
// both states.
func (m model) helpHint() key.Binding {
	desc := "show help"
	if !m.hintsCollapsed {
		desc = "hide help"
	}

	return hint(m.keys.Help.Toggle, desc)
}

// agentHints lists the agents screen's bindings.
func (m model) agentHints() legend {
	showHiddenDesc := "show hidden"
	if m.tree.showHidden {
		showHiddenDesc = "put hidden away"
	}

	rows := m.rowHints()
	full := slices.Concat(rows, m.screenHints(), []key.Binding{
		hint(m.keys.Agents.ShowHidden, showHiddenDesc),
		m.moveHint(),
		hint(m.keys.Agents.Back, "back"),
		m.helpHint(),
	})

	return legend{short: []key.Binding{rows[0], m.helpHint()}, full: full}
}

// rowHints are the bindings that act on the row under the cursor, which is a
// workflow header or an agent.
func (m model) rowHints() []key.Binding {
	if _, hoveringWorkflow := m.hoveredWorkflow(); hoveringWorkflow {
		return []key.Binding{
			hint(m.keys.Agents.New, "new agent"),
			hint(m.keys.Workflows.New, "new workflow"),
			hint(m.keys.Workflows.Rename, "rename"),
			hint(m.keys.Workflows.Edit, "edit"),
			hint(m.keys.Workflows.Delete, "delete"),
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

	return []key.Binding{
		hint(m.keys.Agents.Open, "open"),
		hint(m.keys.Agents.Editor, "editor"),
		hint(m.keys.Agents.Info, infoDesc),
		hint(m.keys.Actions.Open, "actions"),
		hint(m.keys.Agents.Mark, markDesc),
		hint(m.keys.Agents.Hide, hideDesc),
		hint(m.keys.Agents.New, "new agent"),
		hint(m.keys.Workflows.New, "new workflow"),
		hint(m.keys.Agents.Rename, "rename"),
		hint(m.keys.Agents.Archive, "archive"),
	}
}

// screenHints are the screens reachable from anywhere in the tree.
func (m model) screenHints() []key.Binding {
	return []key.Binding{
		hint(m.keys.Agents.Search, "search"),
		hint(m.keys.Schedules.Open, "schedules"),
		hint(m.keys.Archive.Open, "archived agents"),
		hint(m.keys.Agents.Logs, m.logsDesc()),
	}
}

// scheduleHints lists the schedules screen's bindings
func (m model) scheduleHints() legend {
	toggle := "pause"
	if item, ok := m.selectedSchedule(); ok && !item.schedule.Enabled {
		toggle = "resume"
	}

	edit := hint(m.keys.Schedules.Edit, "edit")
	back := hint(m.keys.Schedules.Back, "back")

	return legend{
		short: []key.Binding{edit, back, m.helpHint()},
		full: []key.Binding{
			hint(m.keys.Schedules.New, "new schedule"),
			edit,
			hint(m.keys.Schedules.Toggle, toggle),
			hint(m.keys.Schedules.Delete, "delete"),
			m.moveHint(),
			back,
			m.helpHint(),
		},
	}
}

// archiveHints lists the archive screen's bindings.
func (m model) archiveHints() legend {
	restore := hint(m.keys.Archive.Restore, "restore")
	back := hint(m.keys.Archive.Back, "back")

	return legend{
		short: []key.Binding{restore, back, m.helpHint()},
		full: []key.Binding{
			restore,
			hint(m.keys.Archive.Delete, "delete for good"),
			m.moveHint(),
			back,
			m.helpHint(),
		},
	}
}

// actionHints lists the actions screen's bindings.
func (m model) actionHints() legend {
	runDesc := "run"
	if action, ok := m.selectedAction(); ok && !action.executable {
		runDesc = "chmod +x to run"
	}

	run := hint(m.keys.Actions.Run, runDesc)
	back := hint(m.keys.Actions.Back, "back")

	return legend{
		short: []key.Binding{run, back, m.helpHint()},
		full:  []key.Binding{run, m.moveHint(), back, m.helpHint()},
	}
}

// searchHints lists the search screen's fixed key hints. The screen has no help key, since
// every key on it goes into the query.
func (m model) searchHints() legend {
	open := hint([]string{"enter"}, "open")
	back := hint([]string{"esc"}, "back")

	return legend{
		short: []key.Binding{open, back},
		full:  []key.Binding{open, m.moveHint(), back},
	}
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
