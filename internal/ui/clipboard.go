package ui

import (
	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

// clipboardWriter is how the panel copies a record field's value out, an interface so
// a test can fake it rather than reach the system clipboard.
type clipboardWriter interface {
	WriteAll(text string) error
}

// systemClipboard is the real clipboard: pbcopy, xclip, or clip.exe, whichever the
// platform the panel is actually running on provides.
type systemClipboard struct{}

func (systemClipboard) WriteAll(text string) error { return clipboard.WriteAll(text) }

// tryCopyRecordValue copies the value a digit key names to the clipboard, when key is
// a digit within range of the record currently on screen. It answers false otherwise,
// so the keys that follow it in a screen's switch still get a chance at the press.
func (m model) tryCopyRecordValue(key string) (tea.Cmd, bool) {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return nil, false
	}

	idx := int(key[0] - '1')
	if idx >= len(m.recordValues) {
		return nil, false
	}

	return m.copyCmd(m.recordValues[idx]), true
}

// copyCmd runs the clipboard write as a command rather than inline, since it is the
// one place the panel reaches an external program outside its own API.
func (m model) copyCmd(value string) tea.Cmd {
	return func() tea.Msg {
		return copyResult{err: m.clip.WriteAll(value)}
	}
}

// copyResult is what a clipboard write finished with.
type copyResult struct {
	err error
}
