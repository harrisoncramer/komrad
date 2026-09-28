package ui

import (
	"slices"
	"strings"

	"komrad/internal/ui/paint"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// maxHelpColumns is the widest the expanded legend goes.
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

func (m model) renderHelp(k keyMap, above ...string) string {
	if m.isDetailsFocused() {
		k = m.detailsHelp()
	}
	if m.isTabsFocused() {
		k = m.tabsHelp()
	}
	if m.isKindsFocused() {
		k = m.kindsHelp()
	}
	if m.isMessagesFocused() {
		k = m.messagesHelp()
	}
	if m.filter.typing {
		k = filterHelp(m.filter.mode)
	}
	if m.isViewsOpen() {
		k = m.viewsHelp()
	}

	width := m.paneWidth()
	view := helpView(width)

	if !m.helpExpanded {
		return fitLegend(view.ShortHelpView(byLabel(k.short)), width) + "\n"
	}

	// bubbles truncates a column wider than it was given to a single ellipsis rather than wrapping
	// it, so a legend the sidebar cannot hold gives way to its short form, a width of zero being a
	// panel that has not been told how wide it is and which bubbles reads as no limit.
	columns := helpColumns(byLabel(k.full), width)
	full := fitLegend(view.FullHelpView(columns), width) + "\n"
	if (width > 0 && helpWidth(columns) > width) || !m.doesLegendLeaveARow(full, above) {
		return fitLegend(view.ShortHelpView(m.refusedExpansion(byLabel(k.short))), width) + "\n"
	}

	return full
}

func (m model) doesLegendLeaveARow(legend string, above []string) bool {
	if m.height <= 0 {
		return true
	}

	return m.spareLines(slices.Concat(above, []string{legend})...) >= 1
}

// refusedExpansion re-labels the legend's own key for the short form drawn in place of an
// expansion there was no room for.
func (m model) refusedExpansion(bindings []key.Binding) []key.Binding {
	out := slices.Clone(bindings)
	for i, b := range out {
		if slices.Equal(b.Keys(), m.keys.Help.Toggle) {
			out[i] = binding(m.keys.Help.Toggle, "show help")
		}
	}

	return out
}

func helpView(width int) help.Model {
	view := help.New()
	view.Styles = paint.HelpStyles()
	view.SetWidth(width)

	return view
}

// helpColumns lays the expanded legend out in as many columns as the sidebar can hold, dropping
// a column that does not fit rather than wrapping it.
func helpColumns(bindings []key.Binding, width int) [][]key.Binding {
	for columns := maxHelpColumns; columns > 1; columns-- {
		split := splitColumns(bindings, columns)
		if helpWidth(split) <= width {
			return split
		}
	}

	return [][]key.Binding{bindings}
}

// helpWidth is what the legend wants, measured by the same code that draws it at a width of
// zero, which truncates nothing.
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

// fitLegend cuts each row of the legend to the column it is drawn in.
func fitLegend(rendered string, width int) string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, " ")
		if width > 0 {
			line = ansi.Truncate(line, width, "…")
		}
		lines[i] = line
	}

	return strings.Join(lines, "\n")
}

func binding(keys []string, desc string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keyLabel(keys), desc))
}

func headLabel(keys []string) string {
	if len(keys) == 0 {
		return ""
	}

	return keyLabel(keys[:1])
}

func (m model) moveBinding() key.Binding {
	keys := slices.Concat(m.keys.Nav.Up, m.keys.Nav.Down)

	return key.NewBinding(
		key.WithKeys(keys...),
		key.WithHelp(keyLabel(m.keys.Nav.Up)+"/"+keyLabel(m.keys.Nav.Down), "move"),
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

func filterHelp(mode filterMode) keyMap {
	done := binding([]string{"enter"}, "keep filter")
	flip := binding([]string{"tab"}, mode.flipped().String()+" instead")
	drop := binding([]string{"esc"}, "cancel")

	return keyMap{short: []key.Binding{done, flip, drop}, full: []key.Binding{done, flip, drop}}
}

func (m model) helpBinding() key.Binding {
	desc := "show help"
	if m.helpExpanded {
		desc = "hide help"
	}

	return binding(m.keys.Help.Toggle, desc)
}

func (m model) globalHelp() []key.Binding {
	if !m.screen.picksViews() {
		return []key.Binding{m.helpBinding()}
	}

	return []key.Binding{m.viewsBinding(), m.helpBinding()}
}

// fullGlobalHelp is the expanded legend's share of the keys every screen answers.
func (m model) fullGlobalHelp() []key.Binding {
	return append(m.globalHelp(), binding(slices.Concat(m.keys.Sidebar.Narrow, m.keys.Sidebar.Widen), "resize"))
}

func keyLabel(keys []string) string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = strings.Replace(k, "ctrl+", "C-", 1)
	}

	return strings.Join(out, "/")
}
