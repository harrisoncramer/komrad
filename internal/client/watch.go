package client

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	watchv1 "weave/proto/gen/watch/v1"
	"weave/proto/gen/watch/v1/watchv1connect"
	workflowv1 "weave/proto/gen/workflow/v1"

	"connectrpc.com/connect"
)

const (
	// WatchCoalesce is how long a change waits for the changes behind it before the listing goes
	// out.
	WatchCoalesce = 25 * time.Millisecond
	// WatchSilence is how long a stream may say nothing before it is taken for dead, which is
	// twice the service's heartbeat.
	WatchSilence = 30 * time.Second
	// WatchRetry is the wait before the first reopen, doubling each time up to WatchMaxRetry.
	WatchRetry    = time.Second
	WatchMaxRetry = 15 * time.Second
)

// WatchListing is the three live listings as the watch has built them, each in the order its
// list RPC answers.
type WatchListing struct {
	Agents    []*agentv1.Agent
	Workflows []*workflowv1.Workflow
	Schedules []*schedulev1.Schedule
}

// WatchStream is one open watch, which the stream a connect client answers with is.
type WatchStream interface {
	Receive() bool
	Msg() *watchv1.WatchEvent
	Err() error
	Close() error
}

// WatchParams says how a watch is opened and who is told what it carries.
type WatchParams struct {
	// Open starts one stream.
	Open func(ctx context.Context) (WatchStream, error)
	// OnListing is handed the listing once the snapshot is complete and after each batch of
	// changes.
	OnListing func(WatchListing)
	// OnLost is told a stream has ended, with the error that ended it or nil for a close the
	// service chose, before the next one is opened.
	OnLost func(error)
	// Coalesce, Silence, Retry and MaxRetry take their Watch defaults when zero.
	Coalesce time.Duration
	Silence  time.Duration
	Retry    time.Duration
	MaxRetry time.Duration
}

// OpenWatch is the Open a connect client answers.
func OpenWatch(watches watchv1connect.WatchServiceClient) func(ctx context.Context) (WatchStream, error) {
	return func(ctx context.Context) (WatchStream, error) {
		stream, err := watches.Watch(ctx, connect.NewRequest(&watchv1.WatchRequest{}))
		if err != nil {
			return nil, fmt.Errorf("failed to open the watch: %w", err)
		}

		return stream, nil
	}
}

// Watch holds a watch open for the life of ctx, opening a fresh one after every close and
// building the listing again from the snapshot each one starts with.
func Watch(ctx context.Context, params WatchParams) {
	params = params.withDefaults()
	wait := params.Retry

	for {
		synced, err := watchOnce(ctx, params)
		if ctx.Err() != nil {
			return
		}

		params.OnLost(err)

		if synced {
			wait = params.Retry
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}

		wait = min(wait*2, params.MaxRetry)
	}
}

func (p WatchParams) withDefaults() WatchParams {
	p.Coalesce = cmp.Or(p.Coalesce, WatchCoalesce)
	p.Silence = cmp.Or(p.Silence, WatchSilence)
	p.Retry = cmp.Or(p.Retry, WatchRetry)
	p.MaxRetry = cmp.Or(p.MaxRetry, WatchMaxRetry)

	if p.OnLost == nil {
		p.OnLost = func(error) {}
	}

	return p
}

// watchOnce reads one stream to its end and answers with whether it reached Synced and what
// ended it.
func watchOnce(ctx context.Context, params WatchParams) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)

	stream, err := params.Open(ctx)
	if err != nil {
		cancel()

		return false, err
	}

	// The reader is let go before the stream is closed under it.
	defer func() {
		cancel()

		_ = stream.Close() //nolint:errcheck // the stream is being let go of
	}()

	events := make(chan *watchv1.WatchEvent)
	ended := make(chan error, 1)

	go func() {
		for stream.Receive() {
			select {
			case events <- stream.Msg():
			case <-ctx.Done():
				return
			}
		}

		ended <- stream.Err()
	}()

	held := watchHeld{
		agents:    map[string]*agentv1.Agent{},
		workflows: map[string]*workflowv1.Workflow{},
		schedules: map[string]*schedulev1.Schedule{},
	}

	silence := time.NewTimer(params.Silence)
	defer silence.Stop()

	var (
		synced bool
		flush  <-chan time.Time
	)

	for {
		select {
		case <-ctx.Done():
			return synced, ctx.Err()
		case err := <-ended:
			return synced, err
		case <-silence.C:
			return synced, errors.New("the watch said nothing for " + params.Silence.String())
		case <-flush:
			flush = nil

			params.OnListing(held.listing())
		case event := <-events:
			if !silence.Stop() {
				<-silence.C
			}

			silence.Reset(params.Silence)

			switch event.GetEvent().(type) {
			case *watchv1.WatchEvent_Heartbeat, nil:
			case *watchv1.WatchEvent_Synced:
				synced = true
				flush = nil

				params.OnListing(held.listing())
			case *watchv1.WatchEvent_Agent, *watchv1.WatchEvent_Workflow, *watchv1.WatchEvent_Schedule, *watchv1.WatchEvent_Deleted:
				held.apply(event)

				if synced && flush == nil {
					flush = time.After(params.Coalesce)
				}
			}
		}
	}
}

// watchHeld is the rows one stream has delivered, by id.
type watchHeld struct {
	agents    map[string]*agentv1.Agent
	workflows map[string]*workflowv1.Workflow
	schedules map[string]*schedulev1.Schedule
}

func (h watchHeld) apply(event *watchv1.WatchEvent) {
	switch row := event.GetEvent().(type) {
	case *watchv1.WatchEvent_Agent:
		h.agents[row.Agent.GetId()] = row.Agent
	case *watchv1.WatchEvent_Workflow:
		h.workflows[row.Workflow.GetId()] = row.Workflow
	case *watchv1.WatchEvent_Schedule:
		h.schedules[row.Schedule.GetId()] = row.Schedule
	case *watchv1.WatchEvent_Deleted:
		h.drop(row.Deleted)
	case *watchv1.WatchEvent_Synced, *watchv1.WatchEvent_Heartbeat, nil:
	}
}

func (h watchHeld) drop(deleted *watchv1.Deleted) {
	switch deleted.GetKind() {
	case watchv1.RowKind_ROW_KIND_AGENT:
		delete(h.agents, deleted.GetId())
	case watchv1.RowKind_ROW_KIND_WORKFLOW:
		delete(h.workflows, deleted.GetId())
	case watchv1.RowKind_ROW_KIND_SCHEDULE:
		delete(h.schedules, deleted.GetId())
	case watchv1.RowKind_ROW_KIND_UNSPECIFIED:
	}
}

// listing is the rows in the order the list RPCs answer: agents and schedules newest first,
// workflows by name.
func (h watchHeld) listing() WatchListing {
	return WatchListing{
		Agents: slices.SortedFunc(maps.Values(h.agents), func(a, b *agentv1.Agent) int {
			return strings.Compare(b.GetId(), a.GetId())
		}),
		Workflows: slices.SortedFunc(maps.Values(h.workflows), func(a, b *workflowv1.Workflow) int {
			return cmp.Or(
				strings.Compare(strings.ToLower(a.GetName()), strings.ToLower(b.GetName())),
				strings.Compare(a.GetId(), b.GetId()),
			)
		}),
		Schedules: slices.SortedFunc(maps.Values(h.schedules), func(a, b *schedulev1.Schedule) int {
			return strings.Compare(b.GetId(), a.GetId())
		}),
	}
}
