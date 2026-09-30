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

// toggleCopy puts a stand-in holding the block that has the keys in the sidebar's place in tmux's
// copy-mode, or lets the window move again once the stand-in says copy-mode has ended.
func (m model) toggleCopy() (tea.Model, tea.Cmd) {
	if m.copying {
		return m.endCopy(), nil
	}

	if m.session == nil || m.picker.isActive() {
		return m, nil
	}

	text, row, col := m.drawCopyBlock()
	m.copying = true
	m.frame.Hold()

	session, sidebar := m.session, m.settings.Pane

	return m, func() tea.Msg {
		return copyOpenedMsg{err: session.OpenCopyStandIn(sidebar, text, row, col)}
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

// drawCopyBlock is the rows of the block holding the keys as the sidebar draws them, and the row
// and column its cursor is on, counted from one.
func (m model) drawCopyBlock() (string, int, int) {
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

	var rows []string

	row, col := 0, 1
	for i := rect.Top; i < min(rect.Top+rect.Height, len(lines)); i++ {
		cut := ansi.Cut(lines[i], rect.Left, rect.Left+rect.Width)
		plain := ansi.Strip(cut)

		if at, ok := findCursor(plain); ok && row == 0 {
			row, col = len(rows)+1, at+1
		}

		rows = append(rows, ansi.Truncate(cut, ansi.StringWidth(strings.TrimRight(plain, " ")), "")+ansi.ResetStyle)
	}

	row = max(row, 1)
	for len(rows) > row && ansi.Strip(rows[len(rows)-1]) == "" {
		rows = rows[:len(rows)-1]
	}

	return strings.Join(rows, "\n"), row, col
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
