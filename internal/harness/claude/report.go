package claude

import (
	"encoding/json"
	"fmt"

	"komrade/internal/harness"
)

// The events Claude fires that komrade takes a reading from, named as Claude names them.
const (
	// EventStop is fired as a turn of Claude's own ends.
	EventStop harness.Event = "Stop"

	// EventPreToolUse is fired before a tool call runs, and not again if the call fails.
	EventPreToolUse harness.Event = "PreToolUse"

	// EventPostToolUse is fired once a tool call has returned.
	EventPostToolUse harness.Event = "PostToolUse"

	// EventPermissionRequest is fired when a tool call puts a dialog up for a person to answer.
	EventPermissionRequest harness.Event = "PermissionRequest"

	// EventSessionStart is fired as a conversation starts, resumes, clears or has just compacted.
	EventSessionStart harness.Event = "SessionStart"
)

// CompactMatcher narrows the SessionStart hook to a conversation that has just compacted.
const CompactMatcher = "compact"

// toolPayload is a tool event, of which only the name of the tool is read.
type toolPayload struct {
	ToolName string `json:"tool_name"`
}

// ReadReport reads out of one of Claude's hook payloads only what komrade records about the agent,
// leaving what the hook has to answer Claude with to the subcommand that answers it.
func (h *Harness) ReadReport(event harness.Event, payload []byte) (harness.Report, error) {
	// The event is the caller's rather than the payload's, the subcommand answering an event
	// knowing which one it is.
	switch event {
	// The event says the turn ended on its own, so nothing in the payload decides it, and a
	// turn komrade's own last answer caused is still a turn ending.
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
	case EventPostToolUse, EventSessionStart:
		return harness.Report{}, nil
	default:
		// Refused rather than read as a report of nothing, an empty report clearing the reason on
		// the row.
		return harness.Report{}, fmt.Errorf("no reading is taken from the %q event", event)
	}
}
