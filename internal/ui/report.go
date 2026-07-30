package ui

import "weave/internal/errui"

// report records the outcome of an action, whichever way it went: a nil error clears
// the banner, anything else replaces it. Both halves live here so a caller cannot
// clear without setting, or succeed and leave the previous failure on screen.
func (m *model) report(summary string, err error) {
	if err == nil {
		m.actionErr = nil

		return
	}

	m.actionErr = &errui.Banner{Summary: summary, Detail: errui.Reason(err)}
}
