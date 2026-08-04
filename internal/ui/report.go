package ui

import "weave/internal/errui"

// report is the error-level wrapper around the alert queue: a nil error says nothing,
// since there is nothing to report, and anything else pushes an error alert naming what
// was tried and the reason the service gave. Success no longer clears what is already
// queued, since a queue does not need the clearing a single slot did.
func (m *model) report(summary string, err error) {
	if err == nil {
		return
	}

	m.alerts.push(errui.LevelError, summary, errui.Reason(err))
}
