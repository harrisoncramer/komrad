package ui

import "weave/internal/errui"

// reportBrief stands in for report where the reason is long and already on screen, so the
// banner says what failed without repeating it.
func (m *model) reportBrief(summary, detail string, err error) {
	m.banner = &errui.Banner{
		Summary: summary,
		Detail:  detail,
		Trace:   errui.TraceOf(err),
	}
}

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
