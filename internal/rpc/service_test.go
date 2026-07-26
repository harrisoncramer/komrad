package rpc

import (
	"context"
	"errors"
	"testing"

	"claude-agents/internal/agents"
	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"
	sessionv1 "claude-agents/proto/gen/session/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLauncher records the launch a request asked for and hands back a stored
// chat, so the service can be exercised without a tmux server.
type fakeLauncher struct {
	got       agents.NewSessionParams
	created   *store.Agent
	err       error
	deleted   []string
	deleteErr error
}

// deleted records the names DeleteSession was asked to remove, and deleteErr
// makes it fail, standing in for a chat that is not there.
func (f *fakeLauncher) DeleteSession(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)

	return f.deleteErr
}

func (f *fakeLauncher) NewSession(p agents.NewSessionParams) (*store.Agent, error) {
	f.got = p
	if f.err != nil {
		return nil, f.err
	}

	return f.created, nil
}

func service(t *testing.T) (*SessionService, *store.Store) {
	t.Helper()
	svc, st, _ := serviceWithLauncher(t)

	return svc, st
}

func serviceWithLauncher(t *testing.T) (*SessionService, *store.Store, *fakeLauncher) {
	t.Helper()
	st, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	launcher := &fakeLauncher{created: &store.Agent{ID: "id-1", Name: "spawned", Cwd: "/repo", State: store.StateNew}}

	return NewSessionService(st, launcher), st, launcher
}

func TestCreateSession(t *testing.T) {
	svc, _, launcher := serviceWithLauncher(t)
	workflow := "wf-1"

	res, err := svc.CreateSession(t.Context(), connect.NewRequest(&sessionv1.CreateSessionRequest{
		Prompt:     "audit the migrations",
		Title:      "audit",
		Dir:        "/repo",
		Flags:      "--model opus",
		WorkflowId: &workflow,
	}))
	require.NoError(t, err)

	assert.Equal(t, "audit the migrations", launcher.got.Prompt)
	assert.Equal(t, "audit", launcher.got.Title)
	assert.Equal(t, "/repo", launcher.got.Dir)
	assert.Equal(t, "--model opus", launcher.got.Flags)
	require.NotNil(t, launcher.got.WorkflowID)
	assert.Equal(t, workflow, *launcher.got.WorkflowID)

	assert.Equal(t, "id-1", res.Msg.GetSession().GetId())
	assert.Equal(t, "spawned", res.Msg.GetSession().GetName())
}

// TestCreateSessionUnassignedWorkflow confirms an omitted workflow stays nil
// rather than becoming an empty id, which the store treats as a real workflow.
func TestCreateSessionUnassignedWorkflow(t *testing.T) {
	svc, _, launcher := serviceWithLauncher(t)

	_, err := svc.CreateSession(context.Background(), connect.NewRequest(&sessionv1.CreateSessionRequest{Prompt: "go"}))
	require.NoError(t, err)
	assert.Nil(t, launcher.got.WorkflowID)
}

func TestCreateSessionSurfacesLaunchFailure(t *testing.T) {
	svc, _, launcher := serviceWithLauncher(t)
	launcher.err = errors.New("tmux is not running")

	_, err := svc.CreateSession(context.Background(), connect.NewRequest(&sessionv1.CreateSessionRequest{Prompt: "go"}))
	assert.Equal(t, connect.CodeInternal, connect.CodeOf(err))
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

	list, err := svc.ListSessions(ctx, connect.NewRequest(&sessionv1.ListSessionsRequest{}))
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

// TestDeleteSessionStopsTheAgent confirms the delete goes through the manager,
// which kills the pane, rather than straight to the store, which would leave the
// agent running with nothing able to name it.
func TestDeleteSessionStopsTheAgent(t *testing.T) {
	svc, _, launcher := serviceWithLauncher(t)

	_, err := svc.DeleteSession(context.Background(), connect.NewRequest(&sessionv1.DeleteSessionRequest{Name: "alpha"}))
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha"}, launcher.deleted)
}

// TestMutationsOnMissingTargetsAreNotFound is the regression test for silent
// success: deleting, renaming, or restating a chat that does not exist used to
// return an empty success, so a caller who mistyped a name was told the work was
// done.
func TestMutationsOnMissingTargetsAreNotFound(t *testing.T) {
	svc, _, launcher := serviceWithLauncher(t)
	launcher.deleteErr = agents.ErrSessionNotFound
	ctx := context.Background()

	_, err := svc.DeleteSession(ctx, connect.NewRequest(&sessionv1.DeleteSessionRequest{Name: "ghost"}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	_, err = svc.SetState(ctx, connect.NewRequest(&sessionv1.SetStateRequest{Name: "ghost", State: store.StateDone}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))

	_, err = svc.UpdateSession(ctx, connect.NewRequest(&sessionv1.UpdateSessionRequest{Id: "ghost", Name: "whatever"}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

// TestRenameOntoTakenNameIsAConflict confirms a collision reads as a conflict
// rather than an internal error carrying a raw SQLite constraint message.
func TestRenameOntoTakenNameIsAConflict(t *testing.T) {
	svc, st := service(t)
	ctx := context.Background()
	require.NoError(t, st.Upsert(ctx, store.Agent{Name: "alpha"}))
	require.NoError(t, st.Upsert(ctx, store.Agent{Name: "beta"}))

	beta, err := st.Get(ctx, "beta")
	require.NoError(t, err)
	require.NotNil(t, beta)

	_, err = svc.UpdateSession(ctx, connect.NewRequest(&sessionv1.UpdateSessionRequest{Id: beta.ID, Name: "alpha"}))
	assert.Equal(t, connect.CodeAlreadyExists, connect.CodeOf(err))
	assert.NotContains(t, err.Error(), "UNIQUE constraint", "the driver's message stays server-side")
}
