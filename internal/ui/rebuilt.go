package ui

import (
	"komrad/internal/errui"
)

// BinaryWatcher says whether something replaced the file that started this panel.
type BinaryWatcher interface {
	IsReplaced() bool
}

func (m *model) noticeRebuild() {
	if m.binary == nil || m.isBusy() || !m.binary.IsReplaced() {
		return
	}

	if m.frame != nil {
		m.frame.Vacate()
	}

	m.startupErr = errui.ErrRebuilt
}

func (m *model) isBusy() bool {
	return len(m.writes.inflight) > 0 ||
		m.writes.held.active ||
		m.form.isActive() ||
		m.review.IsBusy()
}
