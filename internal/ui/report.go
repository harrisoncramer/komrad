package ui

import (
	"komrad/internal/errui"
	"komrad/internal/ui/screen"
)

func (m *model) report(summary string, err error) {
	if err == nil {
		return
	}

	m.pushMessage(message{
		kind:    screen.Error,
		summary: summary,
		detail:  errui.GetReason(err),
		trace:   errui.GetTrace(err),
	})
}
