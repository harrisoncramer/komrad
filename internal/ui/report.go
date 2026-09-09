package ui

import "weave/internal/errui"

// Explainer: every failure carrying an error is said here and nowhere else, so a reason is read
// in one place rather than in whichever pane happened to be holding the state it failed against.
// A screen's own notice line is for a refusal that carries no error, such as a search with no
// matches or a press that has to be made twice.

// reportBrief stands in for report where the reason is long and already on screen, so the
// message says what failed without repeating it.
func (m *model) reportBrief(summary, detail string, err error) {
	m.pushMessage(message{
		kind:    messageError,
		summary: summary,
		detail:  detail,
		trace:   errui.GetTrace(err),
	})
}

func (m *model) report(summary string, err error) {
	if err == nil {
		return
	}

	m.pushMessage(message{
		kind:    messageError,
		summary: summary,
		detail:  errui.GetReason(err),
		trace:   errui.GetTrace(err),
	})
}
