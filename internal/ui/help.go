package ui

import (
	"fmt"
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
	if m.tree.selected.active() {
		hovered, _ := rowAt(m.agentRows(), m.tree.cursor)

		return m.selectionHelp(
			m.tree.selected,
			hovered,
			m.markSelectionBinding(),
			binding(m.keys.Agents.Archive, "archive all"),
			m.showHiddenBinding(),
		)
	}

	rows := m.rowHelp()
	full := slices.Concat(rows, m.screenHelp(), []key.Binding{
		m.showHiddenBinding(),
		m.moveBinding(),
		binding(m.keys.Agents.Back, "back"),
		m.helpBinding(),
	})

	return keyMap{short: []key.Binding{rows[0], m.helpBinding()}, full: full}
}

// selectionHelp lists what is live while a selection is up, which both trees show. It
// replaces the row bindings rather than joining them, since those keys stand down in this
// mode, and it names how many rows are held so the count is on screen without a line of its
// own. A screen with a key of its own still live in the mode passes it as an extra.
//
// An empty list hands in the zero row, which carries no id and so reads as unselected.
func (m model) selectionHelp(sel selection, hovered agentRow, extra ...key.Binding) keyMap {
	toggleDesc := "select"
	switch {
	case hovered.header:
		toggleDesc = "select folder"
	case sel.has(hovered.item.agent.ID):
		toggleDesc = "deselect"
	}

	toggle := binding(m.keys.Selection.Toggle, toggleDesc)
	clear := binding(m.keys.Selection.Clear, fmt.Sprintf("clear %d selected", sel.count()))

	full := slices.Concat(
		[]key.Binding{toggle, binding(m.keys.Selection.Range, "select to nearest"), clear},
		extra,
		[]key.Binding{m.moveBinding(), m.helpBinding()},
	)

	return keyMap{short: []key.Binding{toggle, clear, m.helpBinding()}, full: full}
}

// markSelectionBinding is the mark key while a selection is up, named for the glyph the
// press would leave every chosen agent carrying.
func (m model) markSelectionBinding() key.Binding {
	desc := "unmark all"
	if next := nextSelectionMark(selectionMarks(m.tree.selected, m.agentRows()), m.marks); next != nil {
		desc = "mark all " + *next
	}

	return binding(m.keys.Agents.Mark, desc)
}

// showHiddenBinding is the hidden toggle, which names the direction it would go.
func (m model) showHiddenBinding() key.Binding {
	desc := "show hidden"
	if m.tree.showHidden {
		desc = "put hidden away"
	}

	return binding(m.keys.Agents.ShowHidden, desc)
}

// rowHelp are the bindings that act on the row under the cursor, which is a
// workflow header or an agent.
func (m model) rowHelp() []key.Binding {
	if _, hoveringWorkflow := m.hoveredWorkflow(); hoveringWorkflow {
		return []key.Binding{
			binding(m.keys.Agents.New, "new agent"),
			binding(m.keys.Selection.Toggle, "select folder"),
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
		binding(m.keys.Selection.Toggle, "select"),
		binding(m.keys.Agents.Editor, "editor"),
		binding(m.keys.Agents.Terminal, "terminal"),
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
			binding(m.keys.Schedules.Archive, "archive"),
			m.moveBinding(),
			back,
			m.helpBinding(),
		},
	}
}

// archiveHelp lists the archive screen's bindings, which differ by what the cursor is on
// the way the agents screen's do.
func (m model) archiveHelp() keyMap {
	if m.archive.selected.active() {
		hovered, _ := rowAt(m.archiveRows(), m.archive.cursor)

		return m.selectionHelp(
			m.archive.selected,
			hovered,
			binding(m.keys.Archive.Restore, "restore all"),
			binding(m.keys.Archive.Delete, "delete all for good"),
		)
	}

	back := binding(m.keys.Archive.Back, "back")
	rows := m.archiveRowHelp()

	short := []key.Binding{back, m.helpBinding()}
	if len(rows) > 0 {
		short = slices.Concat(rows[:1], short)
	}

	return keyMap{
		short: short,
		full:  slices.Concat(rows, []key.Binding{m.moveBinding(), back, m.helpBinding()}),
	}
}

// archiveRowHelp are the bindings that act on the row under the archive's cursor. A folder
// is restored whole and never destroyed, since nothing in the panel hard-deletes one, and
// a live folder takes no keys at all, since it is drawn only to hold the agents put away
// out of it.
func (m model) archiveRowHelp() []key.Binding {
	if _, ok := m.hoveredArchivedWorkflow(); ok {
		return []key.Binding{
			binding(m.keys.Archive.Restore, "restore workflow"),
			binding(m.keys.Selection.Toggle, "select folder"),
		}
	}
	if _, ok := m.selectedArchived(); !ok {
		return nil
	}

	return []key.Binding{
		binding(m.keys.Archive.Restore, "restore"),
		binding(m.keys.Selection.Toggle, "select"),
		binding(m.keys.Archive.Delete, "delete for good"),
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
	move := binding([]string{"up", "down"}, "move")

	return keyMap{
		short: []key.Binding{open, back},
		full:  []key.Binding{open, move, back},
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
