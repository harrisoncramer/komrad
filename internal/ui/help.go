package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

// maxHelpColumns is the widest the expanded legend goes. A sidebar wide enough for a fourth
// column is wide enough that the legend is already short.
const maxHelpColumns = 3

type keyMap struct {
	short []key.Binding
	full  []key.Binding
}

func byLabel(bindings []key.Binding) []key.Binding {
	sorted := slices.Clone(bindings)
	slices.SortFunc(sorted, func(a, b key.Binding) int {
		return strings.Compare(a.Help().Desc, b.Help().Desc)
	})

	return sorted
}

func (m model) renderHelp(k keyMap) string {
	if m.detailsFocused() {
		k = m.detailsHelp()
	}
	if m.filter.typing {
		k = filterHelp()
	}

	view := helpView(m.width)
	if !m.helpExpanded {
		return trimLines(view.ShortHelpView(byLabel(k.short))) + "\n"
	}

	return trimLines(view.FullHelpView(helpColumns(byLabel(k.full), m.width))) + "\n"
}

func helpView(width int) help.Model {
	view := help.New()
	view.Styles = helpStyles()
	view.Width = width

	return view
}

// helpColumns lays the expanded legend out in as many columns as the sidebar can hold, because
// the rows it takes are the list's. A column that does not fit is dropped whole rather than
// wrapped, so the width is measured before the count is settled and one column is the fallback.
func helpColumns(bindings []key.Binding, width int) [][]key.Binding {
	for columns := maxHelpColumns; columns > 1; columns-- {
		split := splitColumns(bindings, columns)
		if helpWidth(split) <= width {
			return split
		}
	}

	return [][]key.Binding{bindings}
}

// helpWidth is what the legend wants, measured by the same code that draws it. A view with no
// width of its own truncates nothing, so what comes back is the untruncated size.
func helpWidth(columns [][]key.Binding) int {
	return lipgloss.Width(helpView(0).FullHelpView(columns))
}

func splitColumns(bindings []key.Binding, columns int) [][]key.Binding {
	rows := (len(bindings) + columns - 1) / columns

	split := make([][]key.Binding, 0, columns)
	for start := 0; start < len(bindings); start += rows {
		split = append(split, bindings[start:min(start+rows, len(bindings))])
	}

	return split
}

func trimLines(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}

	return strings.Join(lines, "\n")
}

func binding(keys []string, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keyLabel(keys), desc))
}

func (m model) moveBinding() key.Binding {
	keys := slices.Concat(m.keys.Nav.Up, m.keys.Nav.Down)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(keyLabel(m.keys.Nav.Up)+"/"+keyLabel(m.keys.Nav.Down), "move"),
	)
}

func (m model) jumpBinding() key.Binding {
	keys := slices.Concat(m.keys.Nav.Top, m.keys.Nav.Bottom)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(doubledLabel(m.keys.Nav.Top)+"/"+keyLabel(m.keys.Nav.Bottom), "top/bottom"),
	)
}

func doubledLabel(keys []string) string {
	pairs := make([]string, len(keys))
	for i, k := range keys {
		label := keyLabel([]string{k})
		pairs[i] = label + label
	}

	return strings.Join(pairs, "/")
}

func filterHelp() keyMap {
	done := binding([]string{"enter"}, "keep it, then act on what is left")
	drop := binding([]string{"esc"}, "cancel")

	return keyMap{short: []key.Binding{done, drop}, full: []key.Binding{done, drop}}
}

func (m model) filterBinding() key.Binding {
	if m.filter.standing() {
		return binding(m.keys.Filter.Open, "filter: "+m.filter.query)
	}

	return binding(m.keys.Filter.Open, "filter")
}

func (m model) editFilterBinding(keys []string) []key.Binding {
	if !m.filter.standing() {
		return nil
	}

	return []key.Binding{binding(keys, "edit filter")}
}

func (m model) helpBinding() key.Binding {
	desc := "show help"
	if m.helpExpanded {
		desc = "hide help"
	}

	return binding(m.keys.Help.Toggle, desc)
}

func (m model) globalHelp() []key.Binding {
	return []key.Binding{m.helpBinding()}
}

func (m model) agentHelp() keyMap {
	if m.tree.selected.active() {
		hovered, _ := m.hoveredRow()

		return m.selectionHelp(
			m.tree.selected,
			hovered,
			binding(m.keys.Agents.Tag, "cycle tag"),
			binding(m.keys.Agents.Archive, "archive all"),
			m.showHiddenBinding(),
		)
	}

	rows := m.rowHelp()
	full := slices.Concat(
		rows,
		m.screenHelp(),
		[]key.Binding{m.detailsBinding(), m.filterBinding(), m.moveBinding(), m.jumpBinding(), binding(m.keys.Logs.Open, m.logsDesc())},
		m.editFilterBinding(m.keys.Agents.Back),
		m.globalHelp(),
	)

	return keyMap{short: []key.Binding{rows[0], m.helpBinding()}, full: full}
}

func (m model) detailsBinding() key.Binding {
	desc := "details"
	if m.detailsFocused() {
		desc = "back to the list"
	}

	return binding(m.keys.Details.Focus, desc)
}

func (m model) detailsHelp() keyMap {
	copyField := binding(m.keys.Details.Copy, "copy field")
	back := m.detailsBinding()

	return keyMap{
		short: []key.Binding{copyField, back, m.helpBinding()},
		full:  slices.Concat([]key.Binding{copyField, back, m.moveBinding(), m.jumpBinding()}, m.globalHelp()),
	}
}

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
		[]key.Binding{m.moveBinding(), m.jumpBinding()},
		m.globalHelp(),
	)

	return keyMap{short: []key.Binding{toggle, clear, m.helpBinding()}, full: full}
}

func (m model) newBinding() key.Binding {
	keys := slices.Concat(m.keys.Agents.New, m.keys.Workflows.New)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(keyLabel(m.keys.Agents.New)+"/"+keyLabel(m.keys.Workflows.New), "new agent/workflow"),
	)
}

func (m model) showHiddenBinding() key.Binding {
	return binding(m.keys.Agents.ShowHidden, m.showHiddenDesc())
}

func (m model) showHiddenDesc() string {
	if m.tree.showHidden {
		return "put hidden away"
	}

	return "show hidden"
}

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

func (m model) archiveBinding(away []string) key.Binding {
	keys := slices.Concat(away, m.keys.Archive.Open)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(keyLabel(away)+"/"+keyLabel(m.keys.Archive.Open), "archive/archived agents"),
	)
}

func (m model) rowHelp() []key.Binding {
	if _, hoveringWorkflow := m.hoveredWorkflow(); hoveringWorkflow {
		return []key.Binding{
			m.newBinding(),
			binding(m.keys.Workflows.Adopt, "adopt a branch or pr"),
			binding(m.keys.Selection.Toggle, "select folder"),
			binding(m.keys.Workflows.Rename, "rename"),
			binding(m.keys.Workflows.Edit, "edit"),
			m.archiveBinding(m.keys.Workflows.Archive),
			m.showHiddenBinding(),
		}
	}

	// The pr and review keys answer for some agents and not others, and the caveats say which,
	// so that the legend keeps one height as the cursor moves rather than shifting the list.
	return []key.Binding{
		binding(m.keys.Agents.Open, "open"),
		binding(m.keys.Selection.Toggle, "select"),
		binding(m.keys.Actions.Open, "actions"),
		binding(m.keys.Agents.OpenPR, "open pr (if recorded)"),
		binding(m.keys.Agents.Review, "review diff (worktree only)"),
		binding(m.keys.Agents.Tag, "cycle tag"),
		m.hideBinding(),
		m.newBinding(),
		binding(m.keys.Agents.Rename, "rename"),
		m.archiveBinding(m.keys.Agents.Archive),
	}
}

func (m model) screenHelp() []key.Binding {
	return []key.Binding{
		binding(m.keys.Agents.Search, "search"),
		binding(m.keys.Schedules.Open, "schedules"),
	}
}

func (m model) scheduleHelp() keyMap {
	toggle := "pause"
	if item, ok := m.selectedSchedule(); ok && !item.schedule.Enabled {
		toggle = "resume"
	}

	edit := binding(m.keys.Schedules.Edit, "edit")
	editFilter := m.editFilterBinding(m.keys.Schedules.Back)

	return keyMap{
		short: slices.Concat([]key.Binding{edit}, editFilter, []key.Binding{m.helpBinding()}),
		full: slices.Concat([]key.Binding{
			binding(m.keys.Schedules.New, "new schedule"),
			edit,
			binding(m.keys.Schedules.Toggle, toggle),
			binding(m.keys.Schedules.Archive, "archive"),
			m.detailsBinding(),
			m.filterBinding(),
			m.moveBinding(),
		}, editFilter, m.globalHelp()),
	}
}

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

	editFilter := m.editFilterBinding(m.keys.Archive.Back)
	rows := m.archiveRowHelp()

	short := slices.Concat(editFilter, []key.Binding{m.helpBinding()})
	if len(rows) > 0 {
		short = slices.Concat(rows[:1], short)
	}

	return keyMap{
		short: short,
		full: slices.Concat(
			rows,
			[]key.Binding{m.detailsBinding(), m.filterBinding(), m.moveBinding(), m.jumpBinding()},
			editFilter,
			m.globalHelp(),
		),
	}
}

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
		binding(m.keys.Archive.Empty, "empty the archive"),
	}
}

func (m model) actionEditorDesc() string {
	action, ok := m.selectedAction()
	switch {
	case ok && !action.text:
		return "not a text file"
	case ok && m.info.kind == infoEdit && m.info.action == action.name:
		return "close editor"
	default:
		return "edit"
	}
}

func (m model) actionHelp() keyMap {
	runDesc := "run"
	if action, ok := m.selectedAction(); ok && !action.executable {
		runDesc = "chmod +x to run"
	}

	run := binding(m.keys.Actions.Run, runDesc)
	editor := binding(m.keys.Actions.Editor, m.actionEditorDesc())
	editFilter := m.editFilterBinding(m.keys.Actions.Back)

	return keyMap{
		short: slices.Concat([]key.Binding{run, editor}, editFilter, []key.Binding{m.helpBinding()}),
		full: slices.Concat(
			[]key.Binding{run, editor, m.filterBinding(), m.moveBinding()},
			editFilter,
			m.globalHelp(),
		),
	}
}

func (m model) searchHelp() keyMap {
	if m.search.typing {
		done := binding([]string{"enter"}, "keep it, then move through the hits")
		leave := binding([]string{"esc"}, "back")

		return keyMap{short: []key.Binding{done, leave}, full: []key.Binding{done, leave}}
	}

	open := binding([]string{"enter"}, "go to it")
	edit := binding(slices.Concat([]string{"esc"}, m.keys.Filter.Open), "edit the query")

	return keyMap{
		short: []key.Binding{open, edit, m.helpBinding()},
		full: slices.Concat(
			[]key.Binding{open, edit, m.moveBinding(), m.jumpBinding()},
			m.globalHelp(),
		),
	}
}

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
