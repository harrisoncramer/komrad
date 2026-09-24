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

	"weave/db/testmodels"
	"weave/internal/dbtest"
	"weave/internal/interceptors"
	"weave/internal/logging"
	"weave/internal/rpc/render"
	"weave/internal/store"
	"weave/internal/watch"
	"weave/internal/watched"
	"weave/lib/xslice"
	watchv1 "weave/proto/gen/watch/v1"

	agentstore "weave/internal/store/agent"
	rowstore "weave/internal/store/row"
	"weave/proto/gen/watch/v1/watchv1connect"
	watchv1weave "weave/proto/gen/watch/v1/watchv1weave"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// agentStore answers for whichever agents a test names, and for none of the other resources,
// a key prefixed gone- being one that has left the listing.
type agentStore struct {
	ids     []string
	listErr error
}

func (s agentStore) ListWatched(_ context.Context, keys []string) ([]rowstore.Row, error) {
	if keys == nil && s.listErr != nil {
		return nil, s.listErr
	}

	ids := s.ids
	if keys != nil {
		ids = xslice.Filter(keys, func(key string) bool { return !strings.HasPrefix(key, "gone-") })
	}

	return xslice.Map(ids, func(id string) rowstore.Row {
		return rowstore.Row{Agent: agentstore.Agent{ID: id}}
	}), nil
}

// newRows is the read-back the broker renders through, over the agents a test names.
func newRows(agents agentStore) *watch.Rows {
	return watch.NewRows([]watch.Loader{
		watch.NewLoader(watchv1.Resource_RESOURCE_AGENT, agents, func(row rowstore.Row) (string, *watchv1.Row) {
			agent := render.WatchedAgent(row)

			return agent.GetId(), &watchv1.Row{Row: &watchv1.Row_Agent{Agent: agent}}
		}),
	})
}

// served is a watch service on a test server over a database of the test's own, with the log
// it writes.
type served struct {
	broker *watch.Broker
	pool   *pgxpool.Pool
	client watchv1connect.WatchServiceClient
	log    *bytes.Buffer
}

func serve(t *testing.T, agents agentStore, heartbeat time.Duration) *served {
	t.Helper()

	st, err := store.Open(t.Context(), dbtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	pool := st.GetRawPool()

	broker := watch.NewBroker(watch.NewBrokerParams{
		Pool:      pool,
		Rows:      newRows(agents),
		Resources: watched.All(),
	})
	require.NoError(t, broker.Start())
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
		pool:   pool,
		client: watchv1connect.NewWatchServiceClient(server.Client(), server.URL),
		log:    &out,
	}
}

// notify raises one payload on the watch channel the way a trigger would.
func (s *served) notify(t *testing.T, payload string) {
	t.Helper()

	require.NoError(t, testmodels.New(s.pool).NotifyWatch(t.Context(), payload))
}

func receive(t *testing.T, stream *connect.ServerStreamForClient[watchv1.WatchEvent]) *watchv1.WatchEvent {
	t.Helper()

	require.True(t, stream.Receive(), "the stream ended: %v", stream.Err())

	return stream.Msg()
}

// TestWatchSendsTheSnapshotThenChanges pins the shape of the stream end to end, through the
// generated adapter and both interceptors.
func TestWatchSendsTheSnapshotThenChanges(t *testing.T) {
	s := serve(t, agentStore{ids: []string{"a1", "gone-a3"}}, time.Hour)

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	first := receive(t, stream)
	require.Len(t, first.GetSnapshot().GetRows(), 2)
	assert.Equal(t, "a1", first.GetSnapshot().GetRows()[0].GetAgent().GetId())

	s.notify(t, "agents a2")

	written := receive(t, stream)
	require.Len(t, written.GetChanges().GetPuts(), 1)
	assert.Equal(t, "a2", written.GetChanges().GetPuts()[0].GetAgent().GetId())

	s.notify(t, "agents gone-a3")

	deleted := receive(t, stream)
	require.Len(t, deleted.GetChanges().GetDeleted(), 1)
	assert.Equal(t, watchv1.Resource_RESOURCE_AGENT, deleted.GetChanges().GetDeleted()[0].GetResource())
	assert.Equal(t, "gone-a3", deleted.GetChanges().GetDeleted()[0].GetKey())

	require.NoError(t, stream.Close())
}

// TestWatchHeartbeatsWhileNothingChanges pins the heartbeat.
func TestWatchHeartbeatsWhileNothingChanges(t *testing.T) {
	s := serve(t, agentStore{}, 10*time.Millisecond)

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	assert.NotNil(t, receive(t, stream).GetSnapshot())
	assert.NotNil(t, receive(t, stream).GetHeartbeat())
	assert.NotNil(t, receive(t, stream).GetHeartbeat())

	require.NoError(t, stream.Close())
}

// TestWatchEndsWhenTheBrokerCloses pins that stopping the service ends the stream cleanly, and
// that the close is logged with what the stream sent.
func TestWatchEndsWhenTheBrokerCloses(t *testing.T) {
	s := serve(t, agentStore{}, time.Hour)

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	assert.NotNil(t, receive(t, stream).GetSnapshot())

	s.broker.Close()

	assert.False(t, stream.Receive())
	require.NoError(t, stream.Err(), "a stream the service ended is not a failure")

	assert.Eventually(t, func() bool {
		return strings.Contains(s.log.String(), "stream closed") && strings.Contains(s.log.String(), `"rpc.stream.messages_sent":1`)
	}, time.Second, 10*time.Millisecond, "the close is logged with the one message sent:\n%s", s.log.String())
}

// TestWatchRefusedOnceTheBrokerIsClosed pins the refusal a watch gets while the service stops.
func TestWatchRefusedOnceTheBrokerIsClosed(t *testing.T) {
	s := serve(t, agentStore{}, time.Hour)
	s.broker.Close()

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	assert.False(t, stream.Receive())
	assert.Equal(t, connect.CodeFailedPrecondition, connect.CodeOf(stream.Err()))
}

// TestWatchRefusedWhenTheSnapshotFails pins that a snapshot the store could not answer is a
// failure of its own kind rather than the service stopping.
func TestWatchRefusedWhenTheSnapshotFails(t *testing.T) {
	s := serve(t, agentStore{listErr: errors.New("no database")}, time.Hour)

	stream, err := s.client.Watch(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	assert.False(t, stream.Receive())
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(stream.Err()))
	assert.NotContains(t, stream.Err().Error(), "stopping")
}
