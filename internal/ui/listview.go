package ui

import (
	"komrad/internal/glyph"
	"komrad/internal/ui/paint"
	"komrad/internal/view"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// listPane is the column that renders a list this frame and where its cursor stands.
func (m *model) listPane(width int) listPane {
	caret := m.caretOf(sectionList)
	if m.isDocFocused() {
		caret = view.CaretRendered
	}

	return listPane{
		width: width,
		caret: caret,
	}
}

func (m *model) columnListView(params screenListParams) string {
	return m.columnView(func(m *model) string { return m.screenListView(params) })
}

func (m *model) welcomeView(params screenListParams, text string) string {
	if m.term.paneMeasure > 0 {
		return m.screenListView(params)
	}

	full := m.measuredAt(m.term.width)
	header, below := full.listChrome(params)
	lines := full.listLines(header, below)
	centered := lipgloss.NewStyle().Align(lipgloss.Center).Render(text)

	return headedBlockView(header, lipgloss.Place(m.term.width, lines, lipgloss.Center, lipgloss.Center, centered), lines, below)
}

func (m *model) listChrome(params screenListParams) (string, string) {
	header := m.tabbedHeader(params.title, params.tally)
	help := m.renderHelp(m.listHelpOf(params), header)
	record := m.renderRecord(header, help)

	return header, record + help
}

// screenListView renders a screen's list under its header with its record and its legend under
// it.
func (m *model) screenListView(params screenListParams) string {
	header, below := m.listChrome(params)

	if params.rows == 0 && (params.empty != "" || m.filter.doesNarrow(sectionList)) {
		return m.headedListView(header, []string{dimStyle().Render(m.emptyListLine(params.empty))}, 0, below)
	}

	pane := m.paneWidth()
	lines := m.listLines(header, below)
	snapshot := params.snapshot(m.listPane(pane))
	window := view.Window(view.WindowParams{
		List:    prepareList(m.glyphs, snapshot),
		Palette: paint.Panel,
		Width:   pane,
		Lines:   lines,
		Mark:    m.rowMarker(zoneList, snapshot.Rows, pane),
	})

	return headedBlockView(header, window, lines, below)
}

// prepareList resolves a snapshot to the list rendered from it, in the glyphs the panel was
// configured with.
func prepareList(glyphs glyph.Set, snapshot view.Snapshot) view.List {
	snapshot.Glyphs = glyphs

	return view.Prepare(snapshot)
}

// talliedHeader is a list's header with a muted tally after its title.
func (m *model) talliedHeader(title, tally string) string {
	return m.headerLines(title, tally, "")
}

func (m *model) headerLines(title, tally, tabs string) string {
	header := title
	switch {
	case tally == "":
	case header == "":
		header = dimStyle().Render(tally)
	default:
		header += dimStyle().Render("  " + tally)
	}
	if width := m.paneWidth(); width > 0 {
		header = ansi.Truncate(header, width, "…")
	}
	if header != "" {
		header += "\n"
	}

	return m.viewsBar() + header + tabs + m.markSearch(stopList, m.paneWidth(), m.filter.line(m.glyphs)) + "\n\n"
}

// headedListView renders a list under its header, padded out to the record and the legend under it.
func (m *model) headedListView(header string, rows []string, cursor int, help string) string {
	lines := m.listLines(header, help)

	return headedBlockView(header, paint.RenderList(rows, cursor, lines, dimStyle()), lines, help)
}

// emptyListLine is what a screen says over no rows.
func (m *model) emptyListLine(idle string) string {
	return emptyLineOf(m.filter, idle)
}

// emptyLineOf is what a list says over no rows, under the search standing in it.
func emptyLineOf(filter filterState, idle string) string {
	if filter.doesNarrow(sectionList) {
		return "nothing matches"
	}

	return idle
}

// headedBlockView renders a list already fitted to the pane.
func headedBlockView(header, block string, lines int, help string) string {
	return header + paint.PadRows(block, lines) + help
}
