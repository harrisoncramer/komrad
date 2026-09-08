package codex

import (
	"weave/internal/harness"
)

// ReadReport reads nothing, Codex's only report being the notify program it runs at the end of a
// turn.
func (h *Harness) ReadReport(harness.Event, []byte) (harness.Report, error) {
	return harness.Report{}, nil
}
