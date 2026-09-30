package events

import (
	"context"
	"encoding/json"
	"fmt"

	"komrad/internal/mail"
	"komrad/internal/store"

	"github.com/riverqueue/river"
)

// HandlerFor is a Handler over the decoded payloads of one topic.
type HandlerFor[T Payload] func(ctx context.Context, tx store.DBTX, mailer *mail.MailService, payload T) ([]river.JobArgs, error)

// Topic is the topic of T.
func (HandlerFor[T]) Topic() string {
	var payload T

	return payload.Topic()
}

// Handle decodes the payload as a T and hands it to the handler.
func (h HandlerFor[T]) Handle(ctx context.Context, tx store.DBTX, mailer *mail.MailService, raw json.RawMessage) ([]river.JobArgs, error) {
	var payload T
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("failed to read a %s event: %w", payload.Topic(), err)
	}

	return h(ctx, tx, mailer, payload)
}
