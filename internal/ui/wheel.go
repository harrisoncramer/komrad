package ui

import (
	"time"

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

// wheelFlushMsg asks the filter for the turns it held back.
type wheelFlushMsg struct{}

// wheelFilter adds up the turns of the wheel ahead of the update loop and lets one message through
// a frame.
type wheelFilter struct {
	now     func() time.Time
	later   func(tea.Msg)
	last    time.Time
	x       int
	y       int
	rows    int
	flushed bool
}

func newWheelFilter(now func() time.Time, later func(tea.Msg)) *wheelFilter {
	return &wheelFilter{now: now, later: later, flushed: true}
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

	if f.flushed {
		f.flushed = false
		f.later(wheelFlushMsg{})
	}

	return nil
}

func (f *wheelFilter) release() tea.Msg {
	if f.rows == 0 {
		return nil
	}

	f.last = f.now()
	rows := f.rows
	f.rows = 0

	return wheelMsg{x: f.x, y: f.y, rows: rows}
}

// wheelTurn is the row one turn of the wheel moves, zero for a turn that is not up or down.
func wheelTurn(wheel tea.MouseWheelMsg) int {
	switch wheel.Button {
	case tea.MouseWheelUp:
		return -1
	case tea.MouseWheelDown:
		return 1
	default:
		return 0
	}
}

// popupScroller is a screen with a popup the wheel scrolls while the mouse is over it.
type popupScroller interface {
	scrollPopupAt(s panelShared, x, y, rows int) bool
}

// routeWheel scrolls the popup, the block beside the list or the list, whichever the mouse is over.
func (m model) routeWheel(x, y, rows int) (tea.Model, tea.Cmd) {
	if rows == 0 || len(m.wheelKeys(rows)) == 0 {
		return m, nil
	}

	if screen, ok := m.active().(popupScroller); ok && !m.form.isActive() && !m.held.active && screen.scrollPopupAt(m.shared(), x-m.countInsetLeft(), y, rows) {
		return m, nil
	}

	if m.form.isActive() || m.held.active || !m.isOverColumn(x) {
		if screen, ok := m.active().(listScroller); ok {
			return m, screen.scrollList(&m, m.shared(), rows)
		}

		return m.pressWheel(rows)
	}

	if screen, ok := m.active().(columnScroller); ok && screen.doesScrollColumn(m.shared()) {
		return m, screen.scrollColumn(&m, m.shared(), rows*columnWheelStep)
	}

	m.scrollDoc(rows * columnWheelStep)

	return m, nil
}

// pressWheel presses the scroll key once for every row.
func (m model) pressWheel(rows int) (tea.Model, tea.Cmd) {
	pressed := keyMsg(m.wheelKeys(rows)[0])

	var cmds []tea.Cmd
	for range max(rows, -rows) {
		next, cmd := m.route(pressed)
		cmds = append(cmds, cmd)

		routed, ok := next.(model)
		if !ok {
			return next, tea.Batch(cmds...)
		}

		m = routed
	}

	return m, tea.Batch(cmds...)
}

func (m model) wheelKeys(rows int) []string {
	if rows < 0 {
		return m.keys.Column.ScrollUp
	}

	return m.keys.Column.ScrollDown
}
