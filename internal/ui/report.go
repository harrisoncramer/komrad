package ui

import "weave/internal/errui"

// reportBrief stands in for report where the reason is long and already on screen.
func (m *model) reportBrief(summary, detail string) {
	m.pushMessage(message{
		kind:    messageError,
		summary: summary,
		detail:  detail,
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

// reportDropped names the files a silent release said nothing about.
func (m *model) reportDropped(name string, dropped []string) {
	if len(dropped) == 0 {
		return
	}

	m.pushMessage(message{
		kind:    messageWarning,
		summary: name + ": let go in silence with files still open",
		detail: "Nothing was typed at it, so tell it yourself about " + joinList(dropped) +
			" before you trust what it writes next.",
	})
}
