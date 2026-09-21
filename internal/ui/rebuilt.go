package ui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// BinaryWatcher says whether the file this panel was started from has been replaced.
type BinaryWatcher interface {
	IsReplaced() bool
}

const rebuiltSummary = "weave was rebuilt, so this panel is closing"

const rebuiltDetail = "The binary changed under it. The panel key opens a panel from the new build."

func (m model) noticeRebuild() (model, tea.Cmd) {
	if m.binary == nil {
		return m, nil
	}

	if !m.rebuilt {
		if !m.binary.IsReplaced() {
			return m, nil
		}

		m.rebuilt = true
		m.pushMessage(message{
			kind:    messageWarning,
			summary: rebuiltSummary,
			detail:  rebuiltDetail,
		})

		return m, nil
	}

	if m.isBusy() {
		return m, nil
	}

	if m.frame != nil {
		m.frame.Vacate()
	}

	return m, tea.Quit
}

func (m model) isBusy() bool {
	return m.pending > 0 ||
		m.held.active ||
		m.form.isActive() ||
		m.review.isWriting() ||
		m.review.pass.mode != modeWatching ||
		m.review.pass.atEditor
}
