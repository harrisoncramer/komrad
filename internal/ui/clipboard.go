package ui

import (
	"time"

	"weave/internal/ui/layout"

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

func (m *model) setRecord(rec record) {
	m.activeRecord = rec
}

// clearRecord is what a screen with nothing numbered on it sets, so a stray digit
// press finds no record to act on.
func (m *model) clearRecord() {
	m.activeRecord = record{}
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

// flashRecordField flashes the copied field's line in the stage, which is where every
// record the panel draws is drawn.
func (m *model) flashRecordField(idx int) {
	m.frame.Show(layout.RegionStage, newFlashContent(m.activeRecord.markdown, idx+1))
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

// restageScreen redraws whatever the current screen shows for the row or item under its own
// cursor, the same content a screen already draws on every refresh. It is also how the flash
// reverts to plain, and how the stage lays itself out again for the room a closed info
// pane gives back to it.
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
