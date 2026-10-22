package ui

import (
	"image"
	"slices"
	"time"

	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// wheelFrame is how often the wheel's turns reach the panel.
const wheelFrame = time.Second / 60

// wheelMsg is the turns of the wheel one frame added up, down for positive rows and up for negative.
type wheelMsg struct {
	x    int
	y    int
	rows int
}

// wheelHeldMsg says the filter has started holding turns back, which it lets go of on a wheelFlushMsg.
type wheelHeldMsg struct{}

// wheelFlushMsg asks the filter for the turns it held back.
type wheelFlushMsg struct{}

// wheelFilter adds up the turns of the wheel ahead of the update loop and lets one message through
// a frame.
type wheelFilter struct {
	now     func() time.Time
	last    time.Time
	x       int
	y       int
	rows    int
	flushed bool
}

func newWheelFilter(now func() time.Time) *wheelFilter {
	return &wheelFilter{
		now:     now,
		flushed: true,
	}
}

func (f *wheelFilter) filter(_ tea.Model, msg tea.Msg) tea.Msg {
	if _, ok := msg.(wheelFlushMsg); ok {
		f.flushed = true

		return f.release()
	}

	wheel, ok := msg.(tea.MouseWheelMsg)
	if !ok {
		return msg
	}

	turn := wheelTurn(wheel)
	if turn == 0 {
		return msg
	}

	if f.rows*turn < 0 {
		f.rows = 0
	}
	f.rows += turn
	f.x, f.y = wheel.X, wheel.Y

	if f.last.IsZero() || f.now().Sub(f.last) >= wheelFrame {
		return f.release()
	}

	if !f.flushed {
		return nil
	}

	f.flushed = false

	return wheelHeldMsg{}
}

func (f *wheelFilter) release() tea.Msg {
	if f.rows == 0 {
		return nil
	}

	f.last = f.now()
	rows := f.rows
	f.rows = 0

	return wheelMsg{
		x:    f.x,
		y:    f.y,
		rows: rows,
	}
}

// wheelTurn is the row one turn of the wheel moves, zero for a turn that is not up or down.
func wheelTurn(wheel tea.MouseWheelMsg) int {
	if wheel.Button == tea.MouseWheelUp {
		return -1
	}

	if wheel.Button == tea.MouseWheelDown {
		return 1
	}

	return 0
}

// routeWheel hands the keys to whatever the mouse is over and scrolls it.
func (m *model) routeWheel(x, y, rows int) tea.Cmd {
	if rows == 0 || len(m.wheelKeys(rows)) == 0 {
		return nil
	}

	if m.screen == screen.Review && !m.form.isActive() && !m.writes.held.active && !m.isMessagesFocused() && m.review.scrollPopupAt(m, m.inputs(), image.Point{X: x - gutter, Y: y}, rows) {
		return nil
	}

	if m.form.isActive() || m.writes.held.active {
		return m.wheelList(rows)
	}

	if m.screen == screen.Review && !m.isOverColumn(x) {
		if scrolled, ok := m.review.scrollSidebarAt(m, m.inputs(), y, rows); ok {
			return m.apply(scrolled)
		}
	}

	focus := m.focusBlockAt(x, y)

	if !m.isOverColumn(x) {
		return tea.Batch(focus, m.scrollSidebar(rows))
	}

	if screen, ok := m.active().(columnFocuser); ok && screen.doesScrollColumn(m, m.inputs()) {
		return tea.Batch(focus, m.apply(screen.scrollColumn(m, rows*columnWheelStep)))
	}

	m.wheelDoc(rows * columnWheelStep)

	return focus
}

// focusBlockAt hands the keys to the block under the mouse, as jumping to it would.
func (m *model) focusBlockAt(x, y int) tea.Cmd {
	return m.focusBlockIn(m.findWheelBlocks(), x, y)
}

func (m *model) focusBlockIn(blocks []sidebarBlock, x, y int) tea.Cmd {
	for _, block := range slices.Backward(blocks) {
		if !isInRect(block.rect, x, y) {
			continue
		}

		if block.stop == m.atStop() && !m.term.keysElsewhere {
			return nil
		}

		return m.enterStop(block.stop)
	}

	return nil
}

func (m *model) scrollSidebar(rows int) tea.Cmd {
	if m.isDetailsFocused() {
		return m.pressWheel(rows)
	}

	return m.wheelList(rows)
}

// wheelList scrolls the screen's list, or presses the scroll key on a screen without one.
func (m *model) wheelList(rows int) tea.Cmd {
	if screen, ok := m.active().(listScroller); ok {
		return m.apply(screen.scrollList(m, rows))
	}

	return m.pressWheel(rows)
}

// pressWheel presses the scroll key once for every row.
func (m *model) pressWheel(rows int) tea.Cmd {
	pressed := keys.Press(m.wheelKeys(rows)[0])

	var cmds []tea.Cmd
	for range max(rows, -rows) {
		cmds = append(cmds, m.route(pressed))
	}

	return tea.Batch(cmds...)
}

func (m *model) wheelKeys(rows int) []string {
	if rows < 0 {
		return m.keys.Column.ScrollUp
	}

	return m.keys.Column.ScrollDown
}

const wheelGesture = 250 * time.Millisecond

type wheelHit struct {
	blocks []sidebarBlock
	at     time.Time
}

func (m *model) findWheelBlocks() []sidebarBlock {
	now := m.clock.Now()
	if m.wheelHit.blocks == nil || now.Sub(m.wheelHit.at) >= wheelGesture {
		m.wheelHit.blocks = m.findSidebarBlocks()
	}
	m.wheelHit.at = now

	return m.wheelHit.blocks
}
