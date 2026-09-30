package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"komrad/internal/fail"
	"komrad/internal/logging"
	"komrad/internal/mail"
	"komrad/internal/store"

	"github.com/riverqueue/river"
)

// ErrUnknownTopic is an event whose topic no handler is registered for.
var ErrUnknownTopic = fail.Internal("no handler for the event's topic")

// Handler writes the mail the events of one topic call for and returns any jobs to record with them.
type Handler interface {
	Topic() string
	Handle(ctx context.Context, tx store.DBTX, mailer *mail.MailService, payload json.RawMessage) ([]river.JobArgs, error)
}

// NewRouterParams is what NewRouter builds a router from.
type NewRouterParams struct {
	Mailer   *mail.MailService
	Handlers []Handler
}

// Router hands each event to the handler of its topic, in the transaction that marks it done.
type Router struct {
	mailer   *mail.MailService
	handlers map[string]Handler
}

// NewRouter builds the router over the mail service the handlers write through.
func NewRouter(params NewRouterParams) *Router {
	handlers := map[string]Handler{}
	for _, handler := range params.Handlers {
		handlers[handler.Topic()] = handler
	}

	return &Router{mailer: params.Mailer, handlers: handlers}
}

// Route writes the mail one event calls for through tx and returns the jobs to record with them.
func (r *Router) Route(ctx context.Context, tx store.DBTX, event Event) ([]river.JobArgs, error) {
	handler, ok := r.handlers[event.Topic]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownTopic, event.Topic)
	}

	return handler.Handle(ctx, tx, r.mailer.InTx(tx), event.Payload)
}

// DropUnreachable is err unless it says the recipient is archived or gone.
func DropUnreachable(ctx context.Context, recipient string, err error) error {
	if !errors.Is(err, mail.ErrAgentArchived) && !errors.Is(err, mail.ErrRecipientNotFound) {
		return err
	}

	logging.From(ctx).LogAttrs(ctx, slog.LevelInfo, "dropped mail for an agent that is gone",
		append([]slog.Attr{slog.String(logging.AgentID, recipient)}, logging.Err(err)...)...)

	return nil
}
