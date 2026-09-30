package ui

import (
	"strings"
	"unicode"

	"komrad/internal/panes"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// visualLabel is the label tmux draws over a pane in copy-mode.
const visualLabel = " VISUAL "

// copyOpenedMsg is the stand-in opened, or why it could not be.
type copyOpenedMsg struct {
	err error
}

// toggleCopy puts a stand-in drawn as the sidebar in the sidebar's place in tmux's copy-mode, copying
// only from the block that has the keys, or lets the window move again once the stand-in says
// copy-mode has ended.
func (m model) toggleCopy() (tea.Model, tea.Cmd) {
	if m.copying {
		return m.endCopy(), nil
	}

	if m.session == nil || m.picker.isActive() {
		return m, nil
	}

	standIn := m.drawCopyBlock()
	m.copying = true
	m.frame.Hold()

	session, sidebar := m.session, m.settings.Pane

	return m, func() tea.Msg {
		return copyOpenedMsg{err: session.OpenCopyStandIn(sidebar, standIn)}
	}
}

func (m model) applyCopyOpened(msg copyOpenedMsg) model {
	if msg.err == nil || !m.copying {
		return m
	}

	m = m.endCopy()
	m.report("Could not start visual mode", msg.err)

	return m
}

func (m model) endCopy() model {
	m.copying = false
	m.frame.Thaw()

	return m
}

// drawCopyBlock is the sidebar as it draws itself, the block holding the keys, and the row and
// column that block's cursor is on, counted from one.
func (m model) drawCopyBlock() panes.CopyStandIn {
	focused := m.atStop()

	rect := panes.Rect{Width: m.width, Height: m.height}
	for _, block := range m.findSidebarBlocks() {
		if block.stop == focused {
			rect = block.rect
		}
	}

	drawn := m
	drawn.rowCursor.IsBlinked = false
	lines := strings.Split(drawn.drawInset(drawn.insideView()), "\n")

	rows := make([]string, 0, len(lines))
	for _, line := range lines {
		plain := ansi.Strip(line)
		rows = append(rows, ansi.Truncate(line, ansi.StringWidth(strings.TrimRight(plain, " ")), "")+ansi.ResetStyle)
	}

	row, col := rect.Top+1, rect.Left+1
	for i := rect.Top; i < min(rect.Top+rect.Height, len(lines)); i++ {
		plain := ansi.Strip(ansi.Cut(lines[i], rect.Left, rect.Left+rect.Width))
		if at, ok := findCursor(plain); ok {
			row, col = i+1, rect.Left+at+1

			break
		}
	}

	for len(rows) > row && ansi.Strip(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}

	return panes.CopyStandIn{Text: strings.Join(rows, "\n"), Row: row, Col: col, Block: rect}
}

// findCursor is the column a row's cursor is drawn at, found only where it starts the row, which
// a crumb drawn in the same glyph never does.
func findCursor(plain string) (int, bool) {
	before, _, found := strings.Cut(plain, rowCursorGlyph)
	if !found || strings.IndexFunc(before, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
		return 0, false
	}

	return ansi.StringWidth(before), true
}
