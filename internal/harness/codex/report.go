package codex

import (
	"encoding/json"
	"fmt"

	"komrad/internal/harness"
)

// EventNotify is the notify program Codex runs as a turn ends.
const EventNotify harness.Event = "notify"

const turnCompleteType = "agent-turn-complete"

type notifyPayload struct {
	Type string `json:"type"`
}

// ReadReport reads the notify payload Codex hands its notify program at the end of a turn.
func (h *Harness) ReadReport(event harness.Event, payload []byte) (harness.Report, error) {
	if event != EventNotify {
		return harness.Report{}, fmt.Errorf("no reading is taken from the %q event", event)
	}
	if len(payload) == 0 {
		return harness.Report{TurnEnded: true}, nil
	}

	var parsed notifyPayload
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return harness.Report{}, fmt.Errorf("failed to parse the %s payload: %w", event, err)
	}
	if parsed.Type != turnCompleteType {
		return harness.Report{}, harness.ErrNoReport
	}

	return harness.Report{TurnEnded: true}, nil
}
