package claude

import (
	"encoding/json"
	"fmt"

	"weave/internal/harness"
)

// The events Claude fires that weave takes a reading from, named as Claude names them.
const (
	// EventStop is fired as a turn of Claude's own ends.
	EventStop harness.Event = "Stop"

	// EventPreToolUse is fired before a tool call runs, and not again if the call fails.
	EventPreToolUse harness.Event = "PreToolUse"

	// EventPostToolUse is fired once a tool call has returned.
	EventPostToolUse harness.Event = "PostToolUse"

	// EventPermissionRequest is fired when a tool call puts a dialog up for a person to answer.
	EventPermissionRequest harness.Event = "PermissionRequest"
)

// toolPayload is a tool event, of which only the name of the tool is read: an input can carry a
// secret, such as a token on a Bash command line, and whether weave may keep one is undecided.
type toolPayload struct {
	ToolName string `json:"tool_name"`
}

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

	// A tool call and the dialog it may put up are both the same answer, the name of the tool,
	// which is what tells a row blocked on an approval from one blocked on a question.
	case EventPreToolUse, EventPermissionRequest:
		var parsed toolPayload
		if err := json.Unmarshal(payload, &parsed); err != nil {
			return harness.Report{}, fmt.Errorf("failed to parse the %s payload: %w", event, err)
		}

		return harness.Report{Reason: parsed.ToolName}, nil
	case EventPostToolUse:
		return harness.Report{}, nil
	default:
		// Refused rather than read as a report of nothing, since an empty report clears the
		// reason on the row and an event installed without a case here is the mistake that
		// would make.
		return harness.Report{}, fmt.Errorf("no reading is taken from the %q event", event)
	}
}
