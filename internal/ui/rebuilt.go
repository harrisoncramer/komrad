package ui

import (
	"komrad/internal/errui"
)

// BinaryWatcher says whether the file this panel was started from has been replaced.
type BinaryWatcher interface {
	IsReplaced() bool
}

func (m model) noticeRebuild() model {
	if m.binary == nil || m.isBusy() || !m.binary.IsReplaced() {
		return m
	}

	if m.frame != nil {
		m.frame.Vacate()
	}

	m.startupErr = errui.ErrRebuilt

	return m
}

func (m model) isBusy() bool {
	return len(m.inflight) > 0 ||
		m.held.active ||
		m.form.isActive() ||
		m.review.isBusy()
}
