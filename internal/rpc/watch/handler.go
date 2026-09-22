package watch

import (
	"context"
	"errors"
	"time"

	"weave/internal/fail"
	"weave/internal/rpcerr"
	"weave/internal/watch"
	watchv1 "weave/proto/gen/watch/v1"

	"connectrpc.com/connect"
)

// HeartbeatAfter is how long a stream stays silent before a heartbeat goes out.
const HeartbeatAfter = 15 * time.Second

// ErrWatchClosed is a watch asked for while the service is stopping.
var ErrWatchClosed = fail.Precondition("the service is stopping, so no watch can be opened")

type subscriber interface {
	Subscribe(ctx context.Context) (*watch.Subscription, error)
}

// NewHandlerParams is what the handler is built over.
type NewHandlerParams struct {
	Broker subscriber
	// HeartbeatAfter overrides how long a stream stays silent before a heartbeat, HeartbeatAfter
	// when zero.
	HeartbeatAfter time.Duration
}

// Handler serves the watch stream off the broker.
type Handler struct {
	broker         subscriber
	heartbeatAfter time.Duration
}

// NewHandler builds the handler over the broker the database's notifications reach.
func NewHandler(params NewHandlerParams) *Handler {
	after := params.HeartbeatAfter
	if after == 0 {
		after = HeartbeatAfter
	}

	return &Handler{broker: params.Broker, heartbeatAfter: after}
}

// Watch sends the snapshot, then every change until the caller goes away or the
// broker closes the subscription, with a heartbeat whenever nothing has been sent for a while.
func (h *Handler) Watch(ctx context.Context, _ *watchv1.WatchRequest, stream *connect.ServerStream[watchv1.WatchEvent]) *fail.Error {
	subscription, err := h.broker.Subscribe(ctx)
	if errors.Is(err, watch.ErrClosed) {
		return ErrWatchClosed
	}
	if err != nil {
		return rpcerr.From(err, "failed to open the watch")
	}
	defer subscription.Close()

	if err := stream.Send(renderSnapshot(subscription.Snapshot())); err != nil {
		return nil
	}

	heartbeat := time.NewTimer(h.heartbeatAfter)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, open := <-subscription.Events():
			if !open {
				return nil
			}
			if err := stream.Send(renderEvent(event)); err != nil {
				return nil
			}
		case <-heartbeat.C:
			if err := stream.Send(&watchv1.WatchEvent{Event: &watchv1.WatchEvent_Heartbeat{Heartbeat: &watchv1.Heartbeat{}}}); err != nil {
				return nil
			}
		}

		heartbeat.Reset(h.heartbeatAfter)
	}
}

// renderSnapshot is the snapshot as the wire carries it.
func renderSnapshot(rows []*watchv1.Row) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Snapshot{Snapshot: &watchv1.Snapshot{Rows: rows}}}
}

// renderEvent is one event as the wire carries it.
func renderEvent(event watch.Event) *watchv1.WatchEvent {
	if event.Row == nil {
		return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Deleted{Deleted: &watchv1.Deleted{Resource: event.Resource, Key: event.Key}}}
	}

	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Put{Put: event.Row}}
}
