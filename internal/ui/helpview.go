package ui

import (
	"slices"
	"strings"

	"komrad/internal/ui/legend"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/submitting"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// maxHelpColumns is the widest the expanded legend goes.
const maxHelpColumns = 3

func (m *model) Help(k legend.Help, above ...string) string {
	in := m.inputs()
	if m.isDetailsFocused(in) {
		k = m.detailsHelp()
	}
	if in.Room.Doc.Focused {
		k = m.docHelp()
	}
	if m.isTabsFocused(in) {
		k = m.tabsHelp()
	}
	if m.isKindsFocused(in) {
		k = m.kindsHelp()
	}
	if in.Room.MessagesFocused {
		k = m.messagesHelp()
	}
	if m.filter.typing {
		k = filterHelp(m.filter.mode)
	}
	if r, ok := m.active().(renamer); ok && r.isRenaming() {
		k = renameHelp()
	}
	if m.isViewsOpen() {
		k = m.viewsHelp()
	}

	width := m.paneWidth()
	view := helpView(width)
	status := "\n" + m.statusRow(width) + "\n"

	if !m.helpExpanded {
		return fitLegend(view.ShortHelpView(byLabel(k.Short)), width) + status
	}

	// bubbles truncates a column wider than it was given to a single ellipsis rather than wrapping
	// it, so a legend the sidebar cannot hold gives way to its short form, a width of zero being a
	// panel that has not been told how wide it is and which bubbles reads as no limit.
	columns := helpColumns(byLabel(k.Full), width)
	full := fitLegend(view.FullHelpView(columns), width) + status
	if (width > 0 && helpWidth(columns) > width) || !m.doesLegendLeaveARow(full, above) {
		return fitLegend(view.ShortHelpView(m.refusedExpansion(byLabel(k.Short))), width) + status
	}

	return full
}

// statusRow is the row under the legend, holding the writes in flight left of the mode's label.
func (m *model) statusRow(width int) string {
	loose := m.looseWrites()
	if len(loose) == 0 {
		return ""
	}

	const gap = 2

	if width <= 0 {
		return submitting.Badge(loose, m.submittingParams(0))
	}

	room := max(width-lipgloss.Width(normalModeLabel)-gap, 1)
	badge := submitting.Badge(loose, m.submittingParams(room))

	return strings.Repeat(" ", max(room-ansi.StringWidth(badge), 0)) + badge
}

func (m *model) doesLegendLeaveARow(legend string, above []string) bool {
	if m.term.height <= 0 {
		return true
	}

	return m.spareLines(slices.Concat(above, []string{legend})...) >= 1
}

func helpView(width int) help.Model {
	view := help.New()
	view.Styles = paint.Panel.HelpStyles()
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

// helpWidth is what the legend wants, measured by the same code that renders it at a width of
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

// fitLegend cuts each row of the legend to the column that renders it.
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
