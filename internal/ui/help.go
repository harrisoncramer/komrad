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
	return byLabel(k.short)
}

// FullHelp is the expanded help as a single column, so the bindings run down the pane one
// per line rather than side by side in a strip too narrow to hold them.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{byLabel(k.full)}
}

// byLabel orders a legend by what each binding does, so every screen reads alphabetically
// however its bindings were assembled. It sorts a copy, leaving the caller's slice alone.
func byLabel(bindings []key.Binding) []key.Binding {
	sorted := slices.Clone(bindings)
	slices.SortFunc(sorted, func(a, b key.Binding) int {
		return strings.Compare(a.Help().Desc, b.Help().Desc)
	})

	return sorted
}

// renderHelp draws a screen's help, ending in the newline every block a screen builds ends
// in so the height budget counts it like the rest of the views.
func (m model) renderHelp(k keyMap) string {
	// An open filter input has taken the screen's keys, so its legend replaces theirs here
	// rather than at each screen's own help, which would otherwise be four places to forget.
	if m.filter.typing {
		k = filterHelp()
	}

	view := help.New()
	view.Styles = helpStyles()
	view.Width = m.width
	view.ShowAll = m.helpExpanded

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

// jumpBinding is the two ends-of-list keys as one line, the way the nav pair shares one. The
// top key is drawn twice, since it is the second press that acts.
func (m model) jumpBinding() key.Binding {
	keys := slices.Concat(m.keys.Nav.Top, m.keys.Nav.Bottom)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(doubledLabel(m.keys.Nav.Top)+"/"+keyLabel(m.keys.Nav.Bottom), "top/bottom"),
	)
}

// doubledLabel names keys that act on their second press, each drawn as the pair it takes.
func doubledLabel(keys []string) string {
	pairs := make([]string, len(keys))
	for i, k := range keys {
		label := keyLabel([]string{k})
		pairs[i] = label + label
	}

	return strings.Join(pairs, "/")
}

// filterHelp is the legend under a list while the filter input is open. Everything else on the
// screen types into the pattern, so the only key it names is the one that closes it.
func filterHelp() keyMap {
	done := binding([]string{"esc"}, "done, then act on what is left")

	return keyMap{short: []key.Binding{done}, full: []key.Binding{done}}
}

// filterBinding is the key that opens the filter input, naming the pattern already narrowing
// the list so a press reads as editing that rather than starting over.
func (m model) filterBinding() key.Binding {
	if m.filter.standing() {
		return binding(m.keys.Filter.Open, "filter: "+m.filter.query)
	}

	return binding(m.keys.Filter.Open, "filter")
}

// clearFilterBinding is a screen's way out while a filter stands, since the press clears the
// pattern rather than leaving the screen. Leaving needs no line of its own.
func (m model) clearFilterBinding(keys []string) []key.Binding {
	if !m.filter.standing() {
		return nil
	}

	return []key.Binding{binding(keys, "clear filter")}
}

// helpBinding is the key that draws the help itself, which ends every screen's help in both
// states and names the direction it would go.
func (m model) helpBinding() key.Binding {
	desc := "show help"
	if m.helpExpanded {
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
			binding(m.keys.Agents.Mark, "cycle mark"),
			binding(m.keys.Agents.Archive, "archive all"),
			m.showHiddenBinding(),
		)
	}

	rows := m.rowHelp()
	full := slices.Concat(
		rows,
		m.screenHelp(),
		[]key.Binding{m.filterBinding(), m.moveBinding(), m.jumpBinding()},
		m.clearFilterBinding(m.keys.Agents.Back),
		[]key.Binding{m.helpBinding()},
	)

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
		[]key.Binding{m.moveBinding(), m.jumpBinding(), m.helpBinding()},
	)

	return keyMap{short: []key.Binding{toggle, clear, m.helpBinding()}, full: full}
}

// newBinding is the two create keys as one line, an agent inside the folder the cursor is in
// and a folder of its own. They share a line the way the two nav keys do, since the legend is
// already as tall as a short pane holds.
func (m model) newBinding() key.Binding {
	keys := slices.Concat(m.keys.Agents.New, m.keys.Workflows.New)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(keyLabel(m.keys.Agents.New)+"/"+keyLabel(m.keys.Workflows.New), "new agent/workflow"),
	)
}

// showHiddenBinding is the hidden toggle, which names the direction it would go.
func (m model) showHiddenBinding() key.Binding {
	return binding(m.keys.Agents.ShowHidden, m.showHiddenDesc())
}

// showHiddenDesc names the direction the hidden toggle would go.
func (m model) showHiddenDesc() string {
	if m.tree.showHidden {
		return "put hidden away"
	}

	return "show hidden"
}

// hideBinding is the two hidden keys as one line, putting the hovered agent away and drawing
// the ones already put away. They share a line the way the two nav keys do, since a legend as
// tall as a short pane holds has no room for one each.
func (m model) hideBinding() key.Binding {
	keys := slices.Concat(m.keys.Agents.Hide, m.keys.Agents.ShowHidden)

	hide := "hide"
	if item, ok := m.selectedAgent(); ok && item.agent.Hidden {
		hide = "unhide"
	}

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(
			keyLabel(m.keys.Agents.Hide)+"/"+keyLabel(m.keys.Agents.ShowHidden),
			hide+"/"+m.showHiddenDesc(),
		),
	)
}

// archiveBinding is the two archive keys as one line, putting the hovered row away and drawing
// the ones already put away. They share a line the way the two hidden keys do, since the screen
// they open is the one place the key that fills it leads.
func (m model) archiveBinding(away []string) key.Binding {
	keys := slices.Concat(away, m.keys.Archive.Open)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(keyLabel(away)+"/"+keyLabel(m.keys.Archive.Open), "archive/archived agents"),
	)
}

// rowHelp are the bindings that act on the row under the cursor, which is a
// workflow header or an agent.
func (m model) rowHelp() []key.Binding {
	if _, hoveringWorkflow := m.hoveredWorkflow(); hoveringWorkflow {
		return []key.Binding{
			m.newBinding(),
			binding(m.keys.Selection.Toggle, "select folder"),
			binding(m.keys.Workflows.Rename, "rename"),
			binding(m.keys.Workflows.Edit, "edit"),
			m.archiveBinding(m.keys.Workflows.Archive),
			m.showHiddenBinding(),
		}
	}

	infoDesc := "info"
	if m.tree.below == belowRecord {
		infoDesc = "hide info"
	}

	return []key.Binding{
		binding(m.keys.Agents.Open, "open"),
		binding(m.keys.Selection.Toggle, "select"),
		binding(m.keys.Agents.Editor, "editor"),
		binding(m.keys.Agents.Terminal, "terminal"),
		binding(m.keys.Agents.Info, infoDesc),
		binding(m.keys.Actions.Open, "actions"),
		binding(m.keys.Agents.Mark, "cycle mark"),
		m.hideBinding(),
		m.newBinding(),
		binding(m.keys.Agents.Rename, "rename"),
		m.archiveBinding(m.keys.Agents.Archive),
	}
}

// screenHelp are the screens reachable from anywhere in the tree.
func (m model) screenHelp() []key.Binding {
	return []key.Binding{
		binding(m.keys.Agents.Search, "search"),
		binding(m.keys.Schedules.Open, "schedules"),
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
	clear := m.clearFilterBinding(m.keys.Schedules.Back)

	return keyMap{
		short: slices.Concat([]key.Binding{edit}, clear, []key.Binding{m.helpBinding()}),
		full: slices.Concat([]key.Binding{
			binding(m.keys.Schedules.New, "new schedule"),
			edit,
			binding(m.keys.Schedules.Toggle, toggle),
			binding(m.keys.Schedules.Archive, "archive"),
			m.filterBinding(),
			m.moveBinding(),
		}, clear, []key.Binding{m.helpBinding()}),
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

	clear := m.clearFilterBinding(m.keys.Archive.Back)
	rows := m.archiveRowHelp()

	short := slices.Concat(clear, []key.Binding{m.helpBinding()})
	if len(rows) > 0 {
		short = slices.Concat(rows[:1], short)
	}

	return keyMap{
		short: short,
		full: slices.Concat(
			rows,
			[]key.Binding{m.filterBinding(), m.moveBinding(), m.jumpBinding()},
			clear,
			[]key.Binding{m.helpBinding()},
		),
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
	clear := m.clearFilterBinding(m.keys.Actions.Back)

	return keyMap{
		short: slices.Concat([]key.Binding{run}, clear, []key.Binding{m.helpBinding()}),
		full: slices.Concat(
			[]key.Binding{run, m.filterBinding(), m.moveBinding()},
			clear,
			[]key.Binding{m.helpBinding()},
		),
	}
}

// searchHelp lists the search screen's bindings, which differ by whether the query line is
// still open. While it is, every key goes into the query, so the legend names only the fixed
// ones the line leaves alone and there is no help key to expand it.
func (m model) searchHelp() keyMap {
	open := binding([]string{"enter"}, "go to it")

	if !m.search.typing {
		back := binding([]string{"esc"}, "back")
		edit := binding(m.keys.Agents.Search, "edit search")

		return keyMap{
			short: []key.Binding{open, back, m.helpBinding()},
			full:  []key.Binding{open, edit, m.moveBinding(), back, m.helpBinding()},
		}
	}

	move := binding([]string{"up", "down"}, "move")

	var done []key.Binding
	if len(m.search.hits) > 0 {
		done = []key.Binding{binding([]string{"esc"}, "done, then walk the results")}
	}

	return keyMap{
		short: slices.Concat([]key.Binding{open}, done),
		full:  slices.Concat([]key.Binding{open, move}, done),
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
