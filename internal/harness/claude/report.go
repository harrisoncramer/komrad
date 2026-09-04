package claude

import (
	"weave/internal/harness"
)

// EventStop is the event Claude fires as a turn of its own ends.
const EventStop = "Stop"

// ReadReport reads what one of Claude's hook payloads says the agent is doing, taking the event
// from the caller rather than from the payload, since the subcommand answering an event knows
// which one it is and a payload weave cannot parse still arrived on one.
func (h *Harness) ReadReport(event string, _ []byte) (harness.Report, error) {
	switch event {
	// The event says the turn ended on its own, so nothing in the payload decides it, and a
	// turn weave's own last answer caused is still a turn ending.
	case EventStop:
		return harness.Report{TurnEnded: true}, nil
	default:
		return harness.Report{}, nil
	}
}
