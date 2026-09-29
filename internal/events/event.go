// Package events hands each change recorded with its transaction to the handler that mails the
// agents it concerns.
package events

import (
	"encoding/json"
	"fmt"

	"github.com/riverqueue/river"
)

// Queue is the River queue the router works.
const Queue = "events"

// maxAttempts is enough retries to outlast a full mailbox.
const maxAttempts = 20

// Event is one change recorded in the transaction that made it, for the handler of its topic.
type Event struct {
	Topic   string          `json:"topic"`
	Payload json.RawMessage `json:"payload"`
}

// Kind is the River job kind.
func (Event) Kind() string {
	return "event"
}

// InsertOpts puts the event on the router's queue.
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
