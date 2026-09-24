// Package watch is the ConnectRPC transport over the watch coordinator.
package watch

import (
	"context"
	"fmt"
	"time"

	"weave/internal/fail"
	"weave/internal/rpcerr"
	"weave/internal/watch"
	watchv1 "weave/proto/gen/watch/v1"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/emptypb"
)

// HeartbeatAfter is how long a stream stays silent before a heartbeat goes out.
const HeartbeatAfter = 15 * time.Second

// Subscription is one watcher's snapshot and every pass after it.
type Subscription interface {
	Snapshot() []*watchv1.Row
	Changes() <-chan []watch.Change
	Close()
}

type subscriber interface {
	Subscribe(ctx context.Context) (Subscription, error)
}

// BrokerSubscriber opens subscriptions on the broker.
type BrokerSubscriber struct {
	Broker *watch.Broker
}

// Subscribe opens one subscription on the broker.
func (b BrokerSubscriber) Subscribe(ctx context.Context) (Subscription, error) {
	subscription, err := b.Broker.Subscribe(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to the broker: %w", err)
	}

	return subscription, nil
}

// NewHandlerParams is what the handler is built over.
type NewHandlerParams struct {
	Subscriber subscriber
	// HeartbeatAfter overrides how long a stream stays silent before a heartbeat, HeartbeatAfter
	// when zero.
	HeartbeatAfter time.Duration
}

// Handler serves the watch stream off the subscriptions it opens.
type Handler struct {
	subscriber     subscriber
	heartbeatAfter time.Duration
}

// NewHandler builds the handler over what opens its subscriptions.
func NewHandler(params NewHandlerParams) *Handler {
	after := params.HeartbeatAfter
	if after == 0 {
		after = HeartbeatAfter
	}

	return &Handler{subscriber: params.Subscriber, heartbeatAfter: after}
}

// Watch sends the snapshot, then every change until the caller goes away or the
// broker closes the subscription, with a heartbeat whenever nothing has been sent for a while.
func (h *Handler) Watch(ctx context.Context, _ *emptypb.Empty, stream *connect.ServerStream[watchv1.WatchEvent]) *fail.Error {
	subscription, err := h.subscriber.Subscribe(ctx)
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
		case pass, open := <-subscription.Changes():
			if !open {
				return nil
			}
			if err := stream.Send(renderPass(pass)); err != nil {
				return nil
			}
		case <-heartbeat.C:
			if err := stream.Send(&watchv1.WatchEvent{Event: &watchv1.WatchEvent_Heartbeat{Heartbeat: &emptypb.Empty{}}}); err != nil {
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

// renderPass is one pass as the wire carries it.
func renderPass(pass []watch.Change) *watchv1.WatchEvent {
	changes := &watchv1.Changes{}
	for _, change := range pass {
		if change.Row == nil {
			changes.Deleted = append(changes.Deleted, &watchv1.Deleted{Resource: change.Resource, Key: change.Key})

			continue
		}

		changes.Puts = append(changes.Puts, change.Row)
	}

	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Changes{Changes: changes}}
}
