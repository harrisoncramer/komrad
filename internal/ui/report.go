package ui

import "weave/internal/errui"

// reportBrief stands in for report where the reason is long and already on screen, so the message
// says what failed without repeating it.
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
