package ui

import (
	"errors"

	"weave/internal/errui"
)

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

// reportMissingRow says that a conversation the search found has no row to land on, which is
// what enter on a hit for an agent the panel has since dropped leaves behind. Without it the
// cursor simply stays where it was and the key reads as if nothing happened.
func (m *model) reportMissingRow() {
	m.report("Could not go to that conversation", errors.New("the panel holds no row for it"))
}
