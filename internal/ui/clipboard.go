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

// recordSurface is which pane a record is drawn in, so a flash and its revert know
// which one to redraw.
type recordSurface int

const (
	surfaceNone recordSurface = iota
	surfaceMain
	surfaceSecondary
)

func (m *model) setRecord(rec record, surface recordSurface) {
	m.activeRecord = rec
	m.recordSurface = surface
}

// clearRecord is what a screen with nothing numbered on it sets, so a stray digit
// press finds no record to act on.
func (m *model) clearRecord() {
	m.activeRecord = record{}
	m.recordSurface = surfaceNone
}

const flashDuration = 300 * time.Millisecond

// tryCopyRecordValue copies the value a digit key names to the clipboard, when key is
// a digit within range of the record currently on screen, and flashes the field's own
// line while the copy runs.
func (m *model) tryCopyRecordValue(key string) (tea.Cmd, bool) {
	if len(key) != 1 || key[0] < '1' || key[0] > '9' {
		return nil, false
	}

	idx := int(key[0] - '1')
	if idx >= len(m.activeRecord.values) {
		return nil, false
	}

	m.flashRecordField(idx)

	return tea.Batch(m.copyCmd(m.activeRecord.values[idx]), flashRevertTick()), true
}

func (m *model) flashRecordField(idx int) {
	switch m.recordSurface {
	case surfaceMain:
		m.stage.TextFlash(m.activeRecord.markdown, idx+1)
	case surfaceSecondary:
		m.stage.SecondaryTextFlash(m.activeRecord.markdown, idx+1)
	case surfaceNone:
	}
}

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

// applyFlashRevert redraws the current screen's record plainly.
func (m *model) applyFlashRevert() {
	switch m.screen {
	case screenAgents:
		m.stageSelection()
	case screenSchedules:
		m.stageSchedule()
	case screenArchive:
		m.stageArchive()
	default:
		// Not a relevant screen
		return
	}
}
