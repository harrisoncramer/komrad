package codex

import (
	"weave/internal/harness"
)

// ReadReport reads nothing, since Codex's only report is the notify program it runs at the end
// of a turn and that still records the turn itself.
func (h *Harness) ReadReport(harness.Event, []byte) (harness.Report, error) {
	return harness.Report{}, nil
}
