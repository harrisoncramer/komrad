// Package notices turns the events a change records into the notices the agents it concerns
// are sent.
package notices

import (
	"encoding/json"
	"fmt"

	"github.com/riverqueue/river"
)

// Queue is the River queue the router works.
const Queue = "notices"

const maxAttempts = 20

// Event is one change recorded in the transaction that made it, for the handler of its topic.
type Event struct {
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

// Kind is the River job kind.
func (Event) Kind() string {
	return "notice"
}

// InsertOpts puts the event on the router's queue with retries enough to outlast a full mailbox.
func (Event) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       Queue,
		MaxAttempts: maxAttempts,
	}
}

// Payload is what the events of one topic carry.
type Payload interface {
	Topic() string
}

// NewEvent wraps a payload as an event of its topic.
func NewEvent(payload Payload) (*Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to encode a %s event: %w", payload.Topic(), err)
	}

	return &Event{Topic: payload.Topic(), Payload: raw}, nil
}
