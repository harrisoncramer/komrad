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

func (s *scriptedWatch) Watch(ctx context.Context, _ *connect.Request[watchv1.WatchRequest], stream *connect.ServerStream[watchv1.WatchEvent]) error {
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

func agentEvent(seq uint64, id, name string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Seq: seq, Event: &watchv1.WatchEvent_Agent{Agent: &agentv1.Agent{Id: id, Name: name}}}
}

func workflowEvent(seq uint64, id, name string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Seq: seq, Event: &watchv1.WatchEvent_Workflow{Workflow: &workflowv1.Workflow{Id: id, Name: name}}}
}

func scheduleEvent(seq uint64, id string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Seq: seq, Event: &watchv1.WatchEvent_Schedule{Schedule: &schedulev1.Schedule{Id: id}}}
}

func deletedEvent(seq uint64, kind watchv1.RowKind, id string) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Seq: seq, Event: &watchv1.WatchEvent_Deleted{Deleted: &watchv1.Deleted{Kind: kind, Id: id}}}
}

func syncedEvent(seq uint64) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Seq: seq, Event: &watchv1.WatchEvent_Synced{Synced: &watchv1.Synced{}}}
}

func heartbeatEvent(seq uint64) *watchv1.WatchEvent {
	return &watchv1.WatchEvent{Seq: seq, Event: &watchv1.WatchEvent_Heartbeat{Heartbeat: &watchv1.Heartbeat{}}}
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
		case *watchv1.WatchEvent_Agent:
			return row.Agent.GetId()
		case *watchv1.WatchEvent_Workflow:
			return row.Workflow.GetId()
		case *watchv1.WatchEvent_Schedule:
			return row.Schedule.GetId()
		case *watchv1.WatchEvent_Deleted:
			return row.Deleted.GetId()
		case *watchv1.WatchEvent_Synced, *watchv1.WatchEvent_Heartbeat, nil:
		}

		return ""
	})
}

// TestWatchHandsOverTheSnapshotOnceSynced covers the opening of a stream: the rows before
// Synced are held back and go out as one batch marked as the snapshot.
func TestWatchHandsOverTheSnapshotOnceSynced(t *testing.T) {
	t.Parallel()

	scripted := &scriptedWatch{holdLast: true, streams: [][]*watchv1.WatchEvent{{
		agentEvent(1, "01a", "older"),
		agentEvent(1, "01b", "newer"),
		workflowEvent(1, "w1", "Alpha"),
		scheduleEvent(1, "s1"),
		syncedEvent(1),
	}}}

	got := watchOver(t, scripted, WatchParams{})

	require.Eventually(t, func() bool { return got.count() == 1 }, time.Second, 5*time.Millisecond)

	batch := got.at(0)
	assert.True(t, batch.Snapshot)
	assert.Equal(t, []string{"01a", "01b", "w1", "s1"}, ids(batch.Events), "every row before Synced, in order")
	assert.Equal(t, 0, got.lostCount())
}

// TestWatchAppliesChangesAfterSynced covers the live half: a burst of changes goes out as one
// batch, in order, and a heartbeat is not a change.
func TestWatchAppliesChangesAfterSynced(t *testing.T) {
	t.Parallel()

	scripted := &scriptedWatch{holdLast: true, streams: [][]*watchv1.WatchEvent{{
		agentEvent(1, "01a", "one"),
		syncedEvent(1),
		heartbeatEvent(1),
		agentEvent(2, "01a", "renamed"),
		deletedEvent(3, watchv1.RowKind_ROW_KIND_AGENT, "01b"),
		agentEvent(4, "01c", "three"),
	}}}

	got := watchOver(t, scripted, WatchParams{Coalesce: 50 * time.Millisecond})

	require.Eventually(t, func() bool { return got.count() == 2 }, time.Second, 5*time.Millisecond)

	batch := got.at(1)
	assert.False(t, batch.Snapshot)
	assert.Equal(t, []string{"01a", "01b", "01c"}, ids(batch.Events))

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 2, got.count())
}

// TestWatchReopensAfterAClose covers a stream the service closed, which is what a slow
// subscriber dropped for lag or a restart looks like: the next stream's snapshot replaces
// what the last one built.
func TestWatchReopensAfterAClose(t *testing.T) {
	t.Parallel()

	scripted := &scriptedWatch{holdLast: true, streams: [][]*watchv1.WatchEvent{
		{agentEvent(1, "01a", "one"), syncedEvent(1)},
		{agentEvent(7, "01b", "two"), syncedEvent(7)},
	}}

	got := watchOver(t, scripted, WatchParams{})

	require.Eventually(t, func() bool { return got.count() == 2 }, time.Second, 5*time.Millisecond)

	assert.Equal(t, []string{"01a"}, ids(got.at(0).Events))
	assert.True(t, got.at(1).Snapshot, "the next stream opens with a snapshot of its own")
	assert.Equal(t, []string{"01b"}, ids(got.at(1).Events))
	assert.Equal(t, 1, got.lostCount())
	assert.Equal(t, 2, scripted.timesOpened())
}

// TestWatchGivesUpOnASilentStream covers a stream nothing arrives on, which a dead tunnel is:
// it is taken for lost and opened again.
func TestWatchGivesUpOnASilentStream(t *testing.T) {
	t.Parallel()

	scripted := &scriptedWatch{holdLast: true, streams: [][]*watchv1.WatchEvent{
		{agentEvent(1, "01a", "one"), syncedEvent(1)},
		{agentEvent(2, "01a", "one"), syncedEvent(2)},
	}}

	got := watchOver(t, scripted, WatchParams{Silence: 50 * time.Millisecond})

	require.Eventually(t, func() bool { return got.lostCount() >= 1 }, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return scripted.timesOpened() >= 2 }, time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { return got.count() >= 2 }, time.Second, 5*time.Millisecond)
}
