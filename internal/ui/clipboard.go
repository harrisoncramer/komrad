package ui

import (
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

type clipboardWriter interface {
	WriteAll(text string) error
}

// systemClipboard is the real clipboard: pbcopy, xclip, or clip.exe, whichever the
// platform the panel is actually running on provides.
type systemClipboard struct{}

func (systemClipboard) WriteAll(text string) error {
	return clipboard.WriteAll(text)
}

// flashDuration is how long a copied field is held in the accent, long enough to read as a
// deliberate flash rather than a flicker.
const flashDuration = 300 * time.Millisecond

func (m *model) copyCmd(value string) tea.Cmd {
	return func() tea.Msg {
		return copyResult{err: m.clip.WriteAll(value)}
	}
}

// copyResult is what a clipboard write finished with.
type copyResult struct {
	err error
}

func (m *model) applyCopy(result copyResult) {
	m.report("Could not copy to your clipboard", result.err)
}

type flashRevertMsg struct{}

func flashRevertTick() tea.Cmd {
	return tea.Tick(flashDuration, func(time.Time) tea.Msg { return flashRevertMsg{} })
}

// restageScreen redraws whatever the current screen shows for the row or item under its own
// cursor, the same content a screen already draws on every refresh. It is how the stage lays
// itself out again for the room a closed info pane gives back to it.
func (m *model) restageScreen() {
	switch m.screen {
	case screenAgents:
		m.stageSelection()
	case screenSchedules:
		m.stageSchedule()
	case screenArchive:
		m.stageArchive()
	default:
		// Search, the actions list, and the forms put the stage to their own use.
	}
}
