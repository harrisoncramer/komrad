// Package review draws an agent's diff and lets a cursor walk it one line at a time, so a
// line can be lifted back out as an anchor for a review comment.
package review

import (
	"fmt"

	"weave/internal/diff"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// chromeHeight is the header above the diff and the footer under it.
const chromeHeight = 2

// RunParams is what the viewer needs to draw one agent's diff.
type RunParams struct {
	Patch *diff.Patch
}

// Run draws the diff and returns once the viewer has been closed.
func Run(params RunParams) error {
	program := tea.NewProgram(newModel(params.Patch), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("failed to draw the diff: %w", err)
	}

	return nil
}

type model struct {
	patch *diff.Patch
	view  *view

	cursor int
	top    int

	leftColumn int

	width  int
	height int

	err error
}

func newModel(patch *diff.Patch) model {
	v := newView(patch, lipgloss.HasDarkBackground())

	return model{patch: patch, view: v, cursor: max(v.findFirst(), 0)}
}

func (m model) Init() tea.Cmd { return nil }

// Update drives the cursor. Movement is by diff line rather than by screen row, so a header
// is stepped over rather than landed on and every position the cursor can hold is one a
// comment could be attached to.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

		return m.keepCursorVisible(), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	half := max(1, m.getBodyHeight()/2)

	switch key.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.cursor = m.view.move(m.cursor, 1)
	case "k", "up":
		m.cursor = m.view.move(m.cursor, -1)
	case "ctrl+d", "pgdown":
		m.cursor = m.moveBy(half)
	case "ctrl+u", "pgup":
		m.cursor = m.moveBy(-half)
	case "g":
		m.cursor = m.view.findFirst()
	case "G":
		m.cursor = m.moveBy(len(m.view.rows))
	case "]":
		m.cursor = m.view.jumpFile(m.cursor, 1)
	case "[":
		m.cursor = m.view.jumpFile(m.cursor, -1)
	case "l", "right":
		m.leftColumn += horizontalStep
	case "h", "left":
		m.leftColumn = max(0, m.leftColumn-horizontalStep)
	case "0":
		m.leftColumn = 0
	}

	return m.keepCursorVisible(), nil
}

func (m model) moveBy(distance int) int {
	step := 1
	if distance < 0 {
		step = -1
	}

	cursor := m.cursor
	for range distance * step {
		next := m.view.move(cursor, step)
		if next == cursor {
			break
		}

		cursor = next
	}

	return cursor
}

func (m model) keepCursorVisible() model {
	body := m.getBodyHeight()

	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+body {
		m.top = m.cursor - body + 1
	}

	m.top = max(0, min(m.top, max(0, len(m.view.rows)-body)))

	return m
}

func (m model) getBodyHeight() int {
	return max(1, m.height-chromeHeight)
}
