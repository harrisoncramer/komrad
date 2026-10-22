package ui

import (
	"komrad/internal/ui/list"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/screen"
	"komrad/internal/view"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m *model) activeView(in screen.Inputs) string {
	switch s := m.active().(type) {
	case listRenderer:
		return m.listScreenView(s, in)
	case chromeRenderer:
		return s.View(in, m)
	}

	return ""
}

// listScreenView renders a screen as its list beside its own block, or as the list alone on a window
// that will not hold both.
func (m *model) listScreenView(l listRenderer, in screen.Inputs) string {
	params := l.ListParams(in)
	if params.Welcome != "" {
		return m.welcomeView(l, in, params)
	}

	split := in.Room.Split
	if !split.On {
		return m.screenListView(params)
	}

	held := m.measuredAt(split.Sidebar)

	return m.Join(split, held.screenListView(params), held.docColumnView(split))
}

func (m *model) welcomeView(l listRenderer, in screen.Inputs, params list.Params) string {
	if m.term.paneMeasure > 0 {
		return m.screenListView(params)
	}

	full := m.measuredAt(m.term.width)
	header, below := full.listChrome(l.ListParams(in))
	lines := full.Lines(header, below)
	centered := lipgloss.NewStyle().Align(lipgloss.Center).Render(params.Welcome)

	return headedBlockView(header, lipgloss.Place(m.term.width, lines, lipgloss.Center, lipgloss.Center, centered), lines, below)
}

func (m *model) listChrome(params list.Params) (string, string) {
	header := m.tabbedHeader(params.Title, params.Tally)
	help := m.Help(m.listHelp(params.Help), header)
	record := m.Record(header, help)

	return header, record + help
}

// screenListView renders a screen's list under its header with its record and its legend under
// it.
func (m *model) screenListView(params list.Params) string {
	header, below := m.listChrome(params)

	if params.Rows == 0 && (params.Empty != "" || m.filter.pattern().DoesNarrow(list.SectionList)) {
		return m.headedListView(header, []string{paint.Panel.Style(paint.ToneMuted).Render(m.emptyListLine(params.Empty))}, 0, below)
	}

	in := m.inputs()
	pane := in.Room.Width
	lines := m.Lines(header, below)
	snapshot := params.Snapshot(list.Pane{Width: pane, Caret: in.Room.Caret})
	window := view.Window(view.WindowParams{
		List:    list.Prepare(m.glyphs, snapshot),
		Palette: paint.Panel,
		Width:   pane,
		Lines:   lines,
		Mark:    m.rowMarker(zoneList, snapshot.Rows, pane),
	})

	return headedBlockView(header, window, lines, below)
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
		header = paint.Panel.Style(paint.ToneMuted).Render(tally)
	default:
		header += paint.Panel.Style(paint.ToneMuted).Render("  " + tally)
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
	lines := m.Lines(header, help)

	return headedBlockView(header, paint.RenderList(rows, cursor, lines, paint.Panel.Style(paint.ToneMuted)), lines, help)
}

// emptyListLine is what a screen says over no rows.
func (m *model) emptyListLine(idle string) string {
	return emptyLineOf(m.filter.pattern(), idle)
}

// emptyLineOf is what a list says over no rows, under the search standing in it.
func emptyLineOf(pattern list.Pattern, idle string) string {
	if pattern.DoesNarrow(list.SectionList) {
		return "nothing matches"
	}

	return idle
}

// headedBlockView renders a list already fitted to the pane.
func headedBlockView(header, block string, lines int, help string) string {
	return header + paint.PadRows(block, lines) + help
}
