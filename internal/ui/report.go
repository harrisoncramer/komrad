package ui

import "weave/internal/errui"

func (m *model) report(summary string, err error) {
	if err == nil {
		m.banner = nil

		return
	}

	m.banner = &errui.Banner{
		Summary: summary,
		Detail:  errui.Reason(err),
		Trace:   errui.TraceOf(err),
	}
}
