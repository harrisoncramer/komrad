package watch

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"weave/internal/interceptors"
	"weave/internal/logging"
	"weave/internal/store"
	"weave/internal/watch"
	agentv1 "weave/proto/gen/agent/v1"
	watchv1 "weave/proto/gen/watch/v1"
	"weave/proto/gen/watch/v1/watchv1connect"
	watchv1weave "weave/proto/gen/watch/v1/watchv1weave"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type scriptedReader struct {
	snapshot    []watch.Event
	snapshotErr error
}

func (r scriptedReader) Read(_ context.Context, touched watch.Touched) ([]watch.Event, error) {
	var events []watch.Event
	for _, id := range touched.Agents {
		if strings.HasPrefix(id, "gone-") {
			events = append(events, watch.Event{Kind: watch.KindAgent, ID: id})

			continue
		}
		events = append(events, watch.Event{Kind: watch.KindAgent, ID: id, Agent: &agentv1.Agent{Id: id}})
	}

	return events, nil
}

func (r scriptedReader) Snapshot(context.Context) ([]watch.Event, error) {
	return r.snapshot, r.snapshotErr
}

// served is a watch service on a test server, with the log it writes.
type served struct {
	broker *watch.Broker
	client watchv1connect.WatchServiceClient
	log    *bytes.Buffer
}

func serve(t *testing.T, reader watch.Reader, heartbeat time.Duration) *served {
	t.Helper()

	broker := watch.NewBroker(watch.NewBrokerParams{Coalesce: 5 * time.Millisecond})
	broker.Start(reader)
	t.Cleanup(broker.Close)

	var out bytes.Buffer
	validation, err := interceptors.NewValidation()
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.Handle(watchv1connect.NewWatchServiceHandler(
		watchv1weave.WatchServiceAdapter{Impl: NewHandler(NewHandlerParams{Broker: broker, HeartbeatAfter: heartbeat})},
		connect.WithInterceptors(interceptors.NewLogging(logging.New(&out, slog.LevelDebug)), validation),
	))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return &served{
		broker: broker,
		client: watchv1connect.NewWatchServiceClient(server.Client(), server.URL),
		log:    &out,
	}
}

func receive(t *testing.T, stream *connect.ServerStreamForClient[watchv1.WatchEvent]) *watchv1.WatchEvent {
	t.Helper()

	require.True(t, stream.Receive(), "the stream ended: %v", stream.Err())

	return stream.Msg()
}

// TestWatchSendsTheSnapshotThenSyncedThenChanges pins the shape of the stream end to end,
// through the generated adapter and both interceptors.
func TestWatchSendsTheSnapshotThenSyncedThenChanges(t *testing.T) {
	s := serve(t, scriptedReader{snapshot: []watch.Event{
		{Kind: watch.KindAgent, ID: "a1", Agent: &agentv1.Agent{Id: "a1"}},
	}}, time.Hour)

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&watchv1.WatchRequest{}))
	require.NoError(t, err)

	first := receive(t, stream)
	assert.Equal(t, "a1", first.GetAgent().GetId())
	assert.Zero(t, first.GetSeq(), "nothing has rendered yet")
	assert.NotNil(t, receive(t, stream).GetSynced())

	s.broker.Touch(store.TableAgents, "a2", "gone-a3")

	written := receive(t, stream)
	assert.Equal(t, "a2", written.GetAgent().GetId())
	assert.Equal(t, uint64(1), written.GetSeq())

	deleted := receive(t, stream)
	assert.Equal(t, watchv1.RowKind_ROW_KIND_AGENT, deleted.GetDeleted().GetKind())
	assert.Equal(t, "gone-a3", deleted.GetDeleted().GetId())
	assert.Equal(t, uint64(2), deleted.GetSeq())

	require.NoError(t, stream.Close())
}

// TestWatchHeartbeatsWhileNothingChanges pins the heartbeat, and that it does not carry a seq.
func TestWatchHeartbeatsWhileNothingChanges(t *testing.T) {
	s := serve(t, scriptedReader{}, 10*time.Millisecond)

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&watchv1.WatchRequest{}))
	require.NoError(t, err)

	assert.NotNil(t, receive(t, stream).GetSynced())
	assert.NotNil(t, receive(t, stream).GetHeartbeat())
	assert.NotNil(t, receive(t, stream).GetHeartbeat())

	require.NoError(t, stream.Close())
}

// TestWatchEndsWhenTheBrokerCloses pins that stopping the service ends the stream cleanly, and
// that the close is logged with what the stream sent.
func TestWatchEndsWhenTheBrokerCloses(t *testing.T) {
	s := serve(t, scriptedReader{}, time.Hour)

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&watchv1.WatchRequest{}))
	require.NoError(t, err)
	assert.NotNil(t, receive(t, stream).GetSynced())

	s.broker.Close()

	assert.False(t, stream.Receive())
	require.NoError(t, stream.Err(), "a stream the service ended is not a failure")

	assert.Eventually(t, func() bool {
		return strings.Contains(s.log.String(), "stream closed") && strings.Contains(s.log.String(), `"rpc.stream.messages_sent":1`)
	}, time.Second, 10*time.Millisecond, "the close is logged with the one message sent:\n%s", s.log.String())
}

// TestWatchRefusedOnceTheBrokerIsClosed pins the refusal a watch gets while the service stops.
func TestWatchRefusedOnceTheBrokerIsClosed(t *testing.T) {
	s := serve(t, scriptedReader{}, time.Hour)
	s.broker.Close()

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&watchv1.WatchRequest{}))
	require.NoError(t, err)

	assert.False(t, stream.Receive())
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(stream.Err()))
}

// TestWatchRefusedWhenTheSnapshotFails pins that a snapshot the store could not answer is a
// failure of its own kind rather than the service stopping.
func TestWatchRefusedWhenTheSnapshotFails(t *testing.T) {
	s := serve(t, scriptedReader{snapshotErr: errors.New("no database")}, time.Hour)

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&watchv1.WatchRequest{}))
	require.NoError(t, err)

	assert.False(t, stream.Receive())
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(stream.Err()))
	assert.NotContains(t, stream.Err().Error(), "stopping")
}
