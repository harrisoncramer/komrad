package claude

import (
	"fmt"

	"weave/internal/harness"
)

// The events Claude fires that weave takes a reading from, named as Claude names them.
const (
	// EventStop is fired as a turn of Claude's own ends.
	EventStop harness.Event = "Stop"
)

// ReadReport reads out of one of Claude's hook payloads only what weave records about the agent,
// leaving what the hook has to answer Claude with, such as the Stop event's re-entry flag, to the
// subcommand that answers it.
func (h *Harness) ReadReport(event harness.Event, payload []byte) (harness.Report, error) {
	// The event is the caller's rather than the payload's, since the subcommand answering an
	// event knows which one it is and a payload weave cannot parse still arrived on one.
	switch event {
	// The event says the turn ended on its own, so nothing in the payload decides it, and a
	// turn weave's own last answer caused is still a turn ending.
	case EventStop:
		return harness.Report{TurnEnded: true}, nil
	default:
		// Refused rather than read as a report of nothing, since an empty report clears the
		// reason on the row and an event installed without a case here is the mistake that
		// would make.
		return harness.Report{}, fmt.Errorf("no reading is taken from the %q event", event)
	}
}
