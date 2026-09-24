package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atotto/clipboard"
)

type clipboardWriter interface {
	WriteAll(text string) error
}

type systemClipboard struct{}

func (systemClipboard) WriteAll(text string) error {
	return clipboard.WriteAll(text)
}

const flashDuration = 300 * time.Millisecond

func (m *model) copyCmd(value string) tea.Cmd {
	return func() tea.Msg {
		return copyResult{err: m.clip.WriteAll(value)}
	}
}

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

func (m *model) restageScreen() {
	if s, ok := m.active().(staged); ok {
		s.stage(m, m.shared())
	}
}
