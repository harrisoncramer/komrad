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
func renderSnapshot(snapshot watch.Snapshot) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Snapshot{Snapshot: &watchv1.Snapshot{
		Agents:    snapshot.Agents,
		Workflows: snapshot.Workflows,
		Schedules: snapshot.Schedules,
	}}}
}

// renderEvent is one event as the wire carries it.
func renderEvent(event watch.Event) *watchv1.WatchEvent {
	rendered := &watchv1.WatchEvent{}

	switch {
	case event.Agent != nil:
		rendered.Event = &watchv1.WatchEvent_Agent{Agent: event.Agent}
	case event.Workflow != nil:
		rendered.Event = &watchv1.WatchEvent_Workflow{Workflow: event.Workflow}
	case event.Schedule != nil:
		rendered.Event = &watchv1.WatchEvent_Schedule{Schedule: event.Schedule}
	default:
		rendered.Event = &watchv1.WatchEvent_Deleted{Deleted: &watchv1.Deleted{Kind: renderKind(event.Kind), Id: event.ID}}
	}

	return rendered
}

// renderKind is a kind as the wire carries it.
func renderKind(kind watch.Kind) watchv1.RowKind {
	switch kind {
	case watch.KindAgent:
		return watchv1.RowKind_ROW_KIND_AGENT
	case watch.KindWorkflow:
		return watchv1.RowKind_ROW_KIND_WORKFLOW
	case watch.KindSchedule:
		return watchv1.RowKind_ROW_KIND_SCHEDULE
	}

	return watchv1.RowKind_ROW_KIND_UNSPECIFIED
}
