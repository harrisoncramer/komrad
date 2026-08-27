// Package stage draws a file into the panel's stage pane and redraws it when the panel says it
// has changed.
//
// It exists because a pager repaints its whole screen for any change. tmux forwards that to the
// client in a dozen installments spread over tens of milliseconds, which is what reads as a
// flicker while scrolling a diff. Holding the frame in a bubbletea model instead means its
// renderer writes only the lines that actually differ, so moving a cursor down one row costs one
// write rather than a repaint.
package stage

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Run draws the file and blocks until the pane goes away.
func Run(path string) error {
	if _, err := tea.NewProgram(model{path: path}, tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("failed to draw %s: %w", path, err)
	}

	return nil
}

type model struct {
	path string

	frame string

	height int
}

type frameMsg struct {
	frame string
}

func (m model) Init() tea.Cmd { return m.read() }

// read takes the frame off disk. The panel writes the whole screen at once, so a partially
// written file is only ever a frame late, never a torn one.
func (m model) read() tea.Cmd {
	path := m.path

	return func() tea.Msg {
		body, err := os.ReadFile(path)
		if err != nil {
			return frameMsg{}
		}

		return frameMsg{frame: string(body)}
	}
}

// Update redraws whenever a key arrives. The pane has no keyboard of its own: the panel taps a
// key at it once it has rewritten the file, which is the whole protocol between them.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height

		return m, m.read()
	case tea.KeyMsg:
		return m, m.read()
	case frameMsg:
		m.frame = msg.frame

		return m, nil
	}

	return m, nil
}

// View draws the frame, clipped to the pane. Nothing may hand bubbletea more lines than the pane
// holds: it draws what it is given, and a frame taller than the pane scrolls the pane out from
// under it.
func (m model) View() string {
	rows := strings.Split(strings.TrimRight(m.frame, "\n"), "\n")
	if m.height > 0 && len(rows) > m.height {
		rows = rows[:m.height]
	}

	return strings.Join(rows, "\n")
}
