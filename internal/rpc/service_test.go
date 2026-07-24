package rpc

import (
	"context"
	"path/filepath"
	"testing"

	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

func service(t *testing.T) (*SessionService, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "agents.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	return NewSessionService(st), st
}

func TestGetMissingSessionIsNotFound(t *testing.T) {
	svc, _ := service(t)

	_, err := svc.GetSession(context.Background(), connect.NewRequest(&sessionv1.GetSessionRequest{Name: "ghost"}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

func TestListAndGetSession(t *testing.T) {
	svc, st := service(t)
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, store.Agent{Name: "alpha", Cwd: "/tmp", State: store.StateThinking}))

	list, err := svc.ListSessions(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.Len(t, list.Msg.GetSessions(), 1)
	assert.Equal(t, "alpha", list.Msg.GetSessions()[0].GetName())

	got, err := svc.GetSession(ctx, connect.NewRequest(&sessionv1.GetSessionRequest{Name: "alpha"}))
	require.NoError(t, err)
	assert.Equal(t, "/tmp", got.Msg.GetSession().GetCwd())
	assert.Equal(t, store.StateThinking, got.Msg.GetSession().GetState())
}

func TestSetStateThroughService(t *testing.T) {
	svc, st := service(t)
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, store.Agent{Name: "alpha", State: store.StateThinking}))

	_, err := svc.SetState(ctx, connect.NewRequest(&sessionv1.SetStateRequest{Name: "alpha", State: store.StateDone}))
	require.NoError(t, err)

	got, err := st.Get(ctx, "alpha")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, store.StateDone, got.State)
}

func TestUpdateSessionRenamesByID(t *testing.T) {
	svc, st := service(t)
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, store.Agent{Name: "alpha", State: store.StateNew}))
	original, err := st.Get(ctx, "alpha")
	require.NoError(t, err)
	require.NotNil(t, original)

	_, err = svc.UpdateSession(ctx, connect.NewRequest(&sessionv1.UpdateSessionRequest{Id: original.ID, Name: "renamed"}))
	require.NoError(t, err)

	got, err := st.Get(ctx, "renamed")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, original.ID, got.ID, "rename preserves the id")

	stale, err := st.Get(ctx, "alpha")
	require.NoError(t, err)
	assert.Nil(t, stale, "the old name no longer resolves")
}

func TestDeleteSessionThroughService(t *testing.T) {
	svc, st := service(t)
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, store.Agent{Name: "alpha", State: store.StateNew}))

	_, err := svc.DeleteSession(ctx, connect.NewRequest(&sessionv1.DeleteSessionRequest{Name: "alpha"}))
	require.NoError(t, err)

	got, err := st.Get(ctx, "alpha")
	require.NoError(t, err)
	assert.Nil(t, got, "the row is gone after a hard delete")
}
