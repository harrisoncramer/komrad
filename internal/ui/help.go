package ui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

// keyMap is one screen's help in both states: short is the handful worth a line while the
// help is collapsed, and full is everything the screen binds. It satisfies help.KeyMap, so
// bubbles renders either one from the same value.
type keyMap struct {
	short []key.Binding
	full  []key.Binding
}

// ShortHelp is the collapsed help, the one line.
func (k keyMap) ShortHelp() []key.Binding {
	return k.short
}

// FullHelp is the expanded help as a single column, so the bindings run down the pane one
// per line rather than side by side in a strip too narrow to hold them.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.full}
}

// renderHelp draws a screen's help, ending in the newline every block a screen builds ends
// in so the height budget counts it like the rest of the views.
func (m model) renderHelp(k keyMap) string {
	view := help.New()
	view.Styles = helpStyles()
	view.Width = m.width
	view.ShowAll = !m.helpCollapsed

	return trimLines(view.View(k)) + "\n"
}

// trimLines takes the padding off the end of every line of the rendered help. bubbles pads
// a column to its widest line, and in a narrower pane that padding wraps the lines that
// would otherwise have fit.
func trimLines(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}

	return strings.Join(lines, "\n")
}

// binding is one line of the help, a configured key list paired with what it does.
func binding(keys []string, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keyLabel(keys), desc))
}

// moveBinding is the two nav bindings as one line, since neither direction is worth a line
// of its own.
func (m model) moveBinding() key.Binding {
	keys := slices.Concat(m.keys.Nav.Up, m.keys.Nav.Down)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(keyLabel(m.keys.Nav.Up)+"/"+keyLabel(m.keys.Nav.Down), "move"),
	)
}

// helpBinding is the key that draws the help itself, which ends every screen's help in both
// states and names the direction it would go.
func (m model) helpBinding() key.Binding {
	desc := "show help"
	if !m.helpCollapsed {
		desc = "hide help"
	}

	return binding(m.keys.Help.Toggle, desc)
}

// agentHelp lists the agents screen's bindings.
func (m model) agentHelp() keyMap {
	showHiddenDesc := "show hidden"
	if m.tree.showHidden {
		showHiddenDesc = "put hidden away"
	}

	rows := m.rowHelp()
	full := slices.Concat(rows, m.screenHelp(), []key.Binding{
		binding(m.keys.Agents.ShowHidden, showHiddenDesc),
		m.moveBinding(),
		binding(m.keys.Agents.Back, "back"),
		m.helpBinding(),
	})

	return keyMap{short: []key.Binding{rows[0], m.helpBinding()}, full: full}
}

// rowHelp are the bindings that act on the row under the cursor, which is a
// workflow header or an agent.
func (m model) rowHelp() []key.Binding {
	if _, hoveringWorkflow := m.hoveredWorkflow(); hoveringWorkflow {
		return []key.Binding{
			binding(m.keys.Agents.New, "new agent"),
			binding(m.keys.Workflows.New, "new workflow"),
			binding(m.keys.Workflows.Rename, "rename"),
			binding(m.keys.Workflows.Edit, "edit"),
			binding(m.keys.Workflows.Archive, "archive"),
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
		binding(m.keys.Agents.Open, "open"),
		binding(m.keys.Agents.Editor, "editor"),
		binding(m.keys.Agents.Info, infoDesc),
		binding(m.keys.Actions.Open, "actions"),
		binding(m.keys.Agents.Mark, markDesc),
		binding(m.keys.Agents.Hide, hideDesc),
		binding(m.keys.Agents.New, "new agent"),
		binding(m.keys.Workflows.New, "new workflow"),
		binding(m.keys.Agents.Rename, "rename"),
		binding(m.keys.Agents.Archive, "archive"),
	}
}

// screenHelp are the screens reachable from anywhere in the tree.
func (m model) screenHelp() []key.Binding {
	return []key.Binding{
		binding(m.keys.Agents.Search, "search"),
		binding(m.keys.Schedules.Open, "schedules"),
		binding(m.keys.Archive.Open, "archived agents"),
		binding(m.keys.Agents.Logs, m.logsDesc()),
	}
}

// scheduleHelp lists the schedules screen's bindings
func (m model) scheduleHelp() keyMap {
	toggle := "pause"
	if item, ok := m.selectedSchedule(); ok && !item.schedule.Enabled {
		toggle = "resume"
	}

	edit := binding(m.keys.Schedules.Edit, "edit")
	back := binding(m.keys.Schedules.Back, "back")

	return keyMap{
		short: []key.Binding{edit, back, m.helpBinding()},
		full: []key.Binding{
			binding(m.keys.Schedules.New, "new schedule"),
			edit,
			binding(m.keys.Schedules.Toggle, toggle),
			binding(m.keys.Schedules.Delete, "delete"),
			m.moveBinding(),
			back,
			m.helpBinding(),
		},
	}
}

// archiveHelp lists the archive screen's bindings.
func (m model) archiveHelp() keyMap {
	restore := binding(m.keys.Archive.Restore, "restore")
	back := binding(m.keys.Archive.Back, "back")

	return keyMap{
		short: []key.Binding{restore, back, m.helpBinding()},
		full: []key.Binding{
			restore,
			binding(m.keys.Archive.Delete, "delete for good"),
			m.moveBinding(),
			back,
			m.helpBinding(),
		},
	}
}

// actionHelp lists the actions screen's bindings.
func (m model) actionHelp() keyMap {
	runDesc := "run"
	if action, ok := m.selectedAction(); ok && !action.executable {
		runDesc = "chmod +x to run"
	}

	run := binding(m.keys.Actions.Run, runDesc)
	back := binding(m.keys.Actions.Back, "back")

	return keyMap{
		short: []key.Binding{run, back, m.helpBinding()},
		full:  []key.Binding{run, m.moveBinding(), back, m.helpBinding()},
	}
}

// searchHelp lists the search screen's fixed bindings. The screen has no help key, since
// every key on it goes into the query.
func (m model) searchHelp() keyMap {
	open := binding([]string{"enter"}, "open")
	back := binding([]string{"esc"}, "back")

	return keyMap{
		short: []key.Binding{open, back},
		full:  []key.Binding{open, m.moveBinding(), back},
	}
}

// keyLabel renders a binding's keys for the help text, joining them with "/" and shortening
// "ctrl+x" to "C-x". The space bar is named rather than printed, since a literal space reads
// as a binding that failed to load.
func keyLabel(keys []string) string {
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
