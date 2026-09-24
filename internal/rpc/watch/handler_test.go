package watch

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"weave/internal/interceptors"
	"weave/internal/logging"
	"weave/internal/watch"
	"weave/lib/xslice"
	agentv1 "weave/proto/gen/agent/v1"
	watchv1 "weave/proto/gen/watch/v1"

	"weave/proto/gen/watch/v1/watchv1connect"
	watchv1weave "weave/proto/gen/watch/v1/watchv1weave"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeSubscription hands out its snapshot and whatever a test sends on changes.
type fakeSubscription struct {
	snapshot []*watchv1.Row
	changes  chan []watch.Change
	closed   atomic.Bool
}

// Snapshot answers the snapshot.
func (s *fakeSubscription) Snapshot() []*watchv1.Row {
	return s.snapshot
}

// Changes answers the channel a test sends passes on.
func (s *fakeSubscription) Changes() <-chan []watch.Change {
	return s.changes
}

// Close records that the handler let the subscription go.
func (s *fakeSubscription) Close() {
	s.closed.Store(true)
}

// fakeSubscriber opens its one subscription unless err refuses it.
type fakeSubscriber struct {
	subscription *fakeSubscription
	err          error
}

// Subscribe answers the subscription unless err refuses it.
func (f *fakeSubscriber) Subscribe(context.Context) (Subscription, error) {
	if f.err != nil {
		return nil, f.err
	}

	return f.subscription, nil
}

// served is a watch service on a test server over a fake subscriber, with the log it writes.
type served struct {
	subscription *fakeSubscription
	client       watchv1connect.WatchServiceClient
	log          *bytes.Buffer
}

func serve(t *testing.T, subscriber *fakeSubscriber, heartbeat time.Duration) *served {
	t.Helper()

	var out bytes.Buffer
	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.Handle(watchv1connect.NewWatchServiceHandler(
		watchv1weave.WatchServiceAdapter{Impl: NewHandler(NewHandlerParams{Subscriber: subscriber, HeartbeatAfter: heartbeat})},
		connect.WithInterceptors(interceptors.NewLogging(logging.New(&out, slog.LevelDebug)), validation),
	))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return &served{
		subscription: subscriber.subscription,
		client:       watchv1connect.NewWatchServiceClient(server.Client(), server.URL),
		log:          &out,
	}
}

// newSubscriber opens a subscription whose snapshot holds the agents named.
func newSubscriber(ids ...string) *fakeSubscriber {
	return &fakeSubscriber{subscription: &fakeSubscription{
		snapshot: xslice.Map(ids, agentRow),
		changes:  make(chan []watch.Change, 1),
	}}
}

func agentRow(id string) *watchv1.Row {
	return &watchv1.Row{Row: &watchv1.Row_Agent{Agent: &agentv1.Agent{Id: id}}}
}

func receive(t *testing.T, stream *connect.ServerStreamForClient[watchv1.WatchEvent]) *watchv1.WatchEvent {
	t.Helper()

	require.True(t, stream.Receive(), "the stream ended: %v", stream.Err())

	return stream.Msg()
}

// TestWatchSendsTheSnapshotThenChanges pins the shape of the stream end to end, through the
// generated adapter and both interceptors.
func TestWatchSendsTheSnapshotThenChanges(t *testing.T) {
	s := serve(t, newSubscriber("a1", "a3"), time.Hour)

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	first := receive(t, stream)
	require.Len(t, first.GetSnapshot().GetRows(), 2)
	assert.Equal(t, "a1", first.GetSnapshot().GetRows()[0].GetAgent().GetId())

	s.subscription.changes <- []watch.Change{
		{
			Resource: watchv1.Resource_RESOURCE_AGENT,
			Key:      "a2",
			Row:      agentRow("a2"),
		},
		{
			Resource: watchv1.Resource_RESOURCE_AGENT,
			Key:      "a3",
		},
	}

	pass := receive(t, stream)
	require.Len(t, pass.GetChanges().GetPuts(), 1)
	assert.Equal(t, "a2", pass.GetChanges().GetPuts()[0].GetAgent().GetId())
	require.Len(t, pass.GetChanges().GetDeleted(), 1)
	assert.Equal(t, watchv1.Resource_RESOURCE_AGENT, pass.GetChanges().GetDeleted()[0].GetResource())
	assert.Equal(t, "a3", pass.GetChanges().GetDeleted()[0].GetKey())

	require.NoError(t, stream.Close())
	assert.Eventually(t, s.subscription.closed.Load, time.Second, 10*time.Millisecond, "the subscription is let go with the stream")
}

// TestWatchHeartbeatsWhileNothingChanges pins the heartbeat.
func TestWatchHeartbeatsWhileNothingChanges(t *testing.T) {
	s := serve(t, newSubscriber(), 10*time.Millisecond)

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	assert.NotNil(t, receive(t, stream).GetSnapshot())
	assert.NotNil(t, receive(t, stream).GetHeartbeat())
	assert.NotNil(t, receive(t, stream).GetHeartbeat())

	require.NoError(t, stream.Close())
}

// TestWatchEndsWhenTheSubscriptionCloses pins that a subscription the broker ends closes the
// stream cleanly, and that the close is logged with what the stream sent.
func TestWatchEndsWhenTheSubscriptionCloses(t *testing.T) {
	s := serve(t, newSubscriber(), time.Hour)

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	assert.NotNil(t, receive(t, stream).GetSnapshot())

	close(s.subscription.changes)

	assert.False(t, stream.Receive())
	require.NoError(t, stream.Err(), "a stream the service ended is not a failure")

	assert.Eventually(t, func() bool {
		return strings.Contains(s.log.String(), "stream closed") && strings.Contains(s.log.String(), `"rpc.stream.messages_sent":1`)
	}, time.Second, 10*time.Millisecond, "the close is logged with the one message sent:\n%s", s.log.String())
}

// TestWatchRefusalsMapToCodes pins the code each refusal to open a watch reaches the caller as.
func TestWatchRefusalsMapToCodes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode connect.Code
	}{
		{
			name:     "a broker the service has closed",
			err:      watch.ErrClosed,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "a snapshot the store could not answer",
			err:      errors.New("no database"),
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			served := serve(t, &fakeSubscriber{err: tt.err}, time.Hour)

			stream, err := served.client.Watch(t.Context(), connect.NewRequest(&emptypb.Empty{}))
			require.NoError(t, err)

			assert.False(t, stream.Receive())
			assert.Equal(t, tt.wantCode, connect.CodeOf(stream.Err()))
		})
	}
}
