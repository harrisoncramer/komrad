package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	agentv1 "weave/proto/gen/agent/v1"
	schedulev1 "weave/proto/gen/schedule/v1"
	watchv1 "weave/proto/gen/watch/v1"
	"weave/proto/gen/watch/v1/watchv1connect"
	workflowv1 "weave/proto/gen/workflow/v1"

	"weave/lib/xslice"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// scriptedWatch serves one scripted stream per connection, closing each once its events are
// sent and holding the last one open until the test is done.
type scriptedWatch struct {
	mu      sync.Mutex
	streams [][]*watchv1.WatchEvent
	opened  int
	// holdLast keeps the last scripted stream open after its events rather than closing it.
	holdLast bool
}

func (s *scriptedWatch) Watch(ctx context.Context, _ *connect.Request[emptypb.Empty], stream *connect.ServerStream[watchv1.WatchEvent]) error {
	s.mu.Lock()
	at := s.opened
	s.opened++
	s.mu.Unlock()

	if at >= len(s.streams) {
		<-ctx.Done()

		return nil
	}

	for _, event := range s.streams[at] {
		if err := stream.Send(event); err != nil {
			return err
		}
	}

	if s.holdLast && at == len(s.streams)-1 {
		<-ctx.Done()
	}

	return nil
}

func (s *scriptedWatch) timesOpened() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.opened
}

// listings gathers what a watch hands over, in order.
type listings struct {
	mu   sync.Mutex
	got  []WatchBatch
	lost []error
}

func (l *listings) take(batch WatchBatch) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.got = append(l.got, batch)
}

func (l *listings) lose(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.lost = append(l.lost, err)
}

func (l *listings) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.got)
}

func (l *listings) at(i int) WatchBatch {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.got[i]
}

func (l *listings) lostCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.lost)
}

func agentEvent(id, name string) *watchv1.WatchEvent {
	return putEvent(&watchv1.Row{Row: &watchv1.Row_Agent{Agent: &agentv1.Agent{Id: id, Name: name}}})
}

func workflowEvent(id, name string) *watchv1.WatchEvent {
	return putEvent(&watchv1.Row{Row: &watchv1.Row_Workflow{Workflow: &workflowv1.Workflow{Id: id, Name: name}}})
}

func scheduleEvent(id string) *watchv1.WatchEvent {
	return putEvent(&watchv1.Row{Row: &watchv1.Row_Schedule{Schedule: &schedulev1.Schedule{Id: id}}})
}

func putEvent(row *watchv1.Row) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Put{Put: row}}
}

func deletedEvent(resource watchv1.Resource, key string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Deleted{Deleted: &watchv1.Deleted{Resource: resource, Key: key}}}
}

func snapshotEvent(agents ...*agentv1.Agent) *watchv1.WatchEvent {
	rows := xslice.Map(agents, func(agent *agentv1.Agent) *watchv1.Row {
		return &watchv1.Row{Row: &watchv1.Row_Agent{Agent: agent}}
	})

	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Snapshot{Snapshot: &watchv1.Snapshot{Rows: rows}}}
}

func heartbeatEvent() *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Event: &watchv1.WatchEvent_Heartbeat{Heartbeat: &emptypb.Empty{}}}
}

// watchOver runs a watch against the scripted service until the test ends.
func watchOver(t *testing.T, scripted *scriptedWatch, params WatchParams) *listings {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle(watchv1connect.NewWatchServiceHandler(scripted))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	got := &listings{}
	params.Open = OpenWatch(watchv1connect.NewWatchServiceClient(server.Client(), server.URL))
	params.OnBatch = got.take
	params.OnLost = got.lose
	params.Retry = 10 * time.Millisecond
	params.MaxRetry = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		Watch(ctx, params)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return got
}

// ids is the row or tombstone id each event names.
func ids(events []*watchv1.WatchEvent) []string {
	return xslice.Map(events, func(event *watchv1.WatchEvent) string {
		switch row := event.GetEvent().(type) {
		case *watchv1.WatchEvent_Snapshot:
			return "snapshot"
		case *watchv1.WatchEvent_Put:
			return rowID(row.Put)
		case *watchv1.WatchEvent_Deleted:
			return row.Deleted.GetKey()
		case *watchv1.WatchEvent_Heartbeat, nil:
		}

		return ""
	})
}

// TestWatchHandsOverEventsInBatches covers the stream from its opening: the snapshot and the
// changes on its heels go out as one batch, a later burst as another, and a heartbeat is not
// a change.
func TestWatchHandsOverEventsInBatches(t *testing.T) {
	t.Parallel()

	scripted := &scriptedWatch{holdLast: true, streams: [][]*watchv1.WatchEvent{{
		snapshotEvent(&agentv1.Agent{Id: "01a", Name: "one"}),
		agentEvent("01a", "renamed"),
		heartbeatEvent(),
		deletedEvent(watchv1.Resource_RESOURCE_AGENT, "01b"),
		scheduleEvent("s1"),
		workflowEvent("w1", "Alpha"),
	}}}

	got := watchOver(t, scripted, WatchParams{Coalesce: 50 * time.Millisecond})

	require.Eventually(t, func() bool { return got.count() == 1 }, time.Second, 5*time.Millisecond)

	assert.Equal(t, []string{"snapshot", "01a", "01b", "s1", "w1"}, ids(got.at(0)), "every event, in order, with the heartbeat left out")
	assert.Equal(t, 0, got.lostCount())

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, got.count())
}

// TestWatchReopensAfterAClose covers a stream the service closed, which is what a slow
// subscriber dropped for lag or a restart looks like: the next stream's snapshot replaces
// what the last one built.
func TestWatchReopensAfterAClose(t *testing.T) {
	t.Parallel()

	scripted := &scriptedWatch{holdLast: true, streams: [][]*watchv1.WatchEvent{
		{snapshotEvent(), agentEvent("01a", "one")},
		{snapshotEvent(), agentEvent("01b", "two")},
	}}

	got := watchOver(t, scripted, WatchParams{})

	require.Eventually(t, func() bool { return got.count() == 2 }, time.Second, 5*time.Millisecond)

	assert.Equal(t, []string{"snapshot", "01a"}, ids(got.at(0)))
	assert.Equal(t, []string{"snapshot", "01b"}, ids(got.at(1)), "the next stream opens with a snapshot of its own")
	assert.Equal(t, 1, got.lostCount())
	assert.Equal(t, 2, scripted.timesOpened())
}

// TestWatchGivesUpOnASilentStream covers a stream nothing arrives on, which a dead tunnel is:
// it is taken for lost and opened again.
func TestWatchGivesUpOnASilentStream(t *testing.T) {
	t.Parallel()

	scripted := &scriptedWatch{holdLast: true, streams: [][]*watchv1.WatchEvent{
		{snapshotEvent()},
		{snapshotEvent()},
	}}

	got := watchOver(t, scripted, WatchParams{Silence: 50 * time.Millisecond})

	require.Eventually(t, func() bool { return got.lostCount() >= 1 }, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return scripted.timesOpened() >= 2 }, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return got.count() >= 2 }, time.Second, 5*time.Millisecond)
}

// rowID is the id of whichever resource a row is.
func rowID(row *watchv1.Row) string {
	switch row := row.GetRow().(type) {
	case *watchv1.Row_Agent:
		return row.Agent.GetId()
	case *watchv1.Row_Workflow:
		return row.Workflow.GetId()
	case *watchv1.Row_Schedule:
		return row.Schedule.GetId()
	case nil:
	}

	return ""
}
