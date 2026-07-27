package ui

import "claude-agents/internal/errui"

// report records the outcome of a write, whichever way it went: a nil error clears
// the banner, anything else replaces it. Both halves live here so a caller cannot
// clear without setting, or succeed and leave the previous failure on screen.
func (m *model) report(summary string, err error) {
	if err == nil {
		m.actionErr = nil

		return
	}

	m.failAction(summary, err)
}

// failAction sets the banner without the clearing half, for a failure found
// somewhere that also runs after a successful write. Every action reloads the
// lists, so a reload that cleared on success would wipe the error the action it
// followed had just reported.
func (m *model) failAction(summary string, err error) {
	m.actionErr = &errui.Banner{Summary: summary, Detail: errui.Reason(err)}
}
