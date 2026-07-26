package rpc

import (
	"testing"

	"claude-agents/internal/agents"
	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"
	paginationv1 "claude-agents/proto/gen/pagination/v1"
	workflowv1 "claude-agents/proto/gen/workflow/v1"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func workflowService(t *testing.T) (*WorkflowService, *store.Store) {
	t.Helper()

	st, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	return NewWorkflowService(st, agents.NewWorkflowService(st)), st
}

func TestCreateAndGetWorkflow(t *testing.T) {
	svc, _ := workflowService(t)

	created, err := svc.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:    "backend",
		Context: "shared notes",
		Cwd:     "~/repo",
		Flags:   "--model opus",
	}))
	require.NoError(t, err)

	id := created.Msg.GetWorkflow().GetId()
	require.NotEmpty(t, id)
	assert.Equal(t, "backend", created.Msg.GetWorkflow().GetName())
	assert.Equal(t, "shared notes", created.Msg.GetWorkflow().GetContext())
	assert.NotEqual(t, "~/repo", created.Msg.GetWorkflow().GetCwd(), "the service expands ~ before storing")

	got, err := svc.GetWorkflow(t.Context(), connect.NewRequest(&workflowv1.GetWorkflowRequest{Id: id}))
	require.NoError(t, err)
	assert.Equal(t, "backend", got.Msg.GetWorkflow().GetName())
}

func TestGetMissingWorkflowIsNotFound(t *testing.T) {
	svc, _ := workflowService(t)

	_, err := svc.GetWorkflow(t.Context(), connect.NewRequest(&workflowv1.GetWorkflowRequest{
		Id: "00000000-0000-0000-0000-000000000000",
	}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

// TestUpdateWorkflowReplacesFields confirms an update writes every field, which
// is what the request says it does, and that an unknown id is not_found rather
// than a silent success.
func TestUpdateWorkflowReplacesFields(t *testing.T) {
	svc, st := workflowService(t)

	created, err := svc.CreateWorkflow(t.Context(), connect.NewRequest(&workflowv1.CreateWorkflowRequest{
		Name:    "backend",
		Context: "original",
	}))
	require.NoError(t, err)
	id := created.Msg.GetWorkflow().GetId()

	_, err = svc.UpdateWorkflow(t.Context(), connect.NewRequest(&workflowv1.UpdateWorkflowRequest{
		Id:      id,
		Name:    "platform",
		Context: "rewritten",
		Cwd:     "/repo",
	}))
	require.NoError(t, err)

	stored, err := st.GetWorkflow(t.Context(), id)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "platform", stored.Name)
	assert.Equal(t, "rewritten", stored.Context)
	assert.Equal(t, "/repo", stored.Cwd)

	_, err = svc.UpdateWorkflow(t.Context(), connect.NewRequest(&workflowv1.UpdateWorkflowRequest{
		Id:   "00000000-0000-0000-0000-000000000000",
		Name: "ghost",
	}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

// TestDeleteWorkflowOrphansOrArchives covers the one decision the request
// carries: whether the chats inside go with the workflow or outlive it.
func TestDeleteWorkflowOrphansOrArchives(t *testing.T) {
	svc, st := workflowService(t)
	ctx := t.Context()

	created, err := svc.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{Name: "backend"}))
	require.NoError(t, err)
	id := created.Msg.GetWorkflow().GetId()

	require.NoError(t, st.Upsert(ctx, store.Agent{Name: "chat", WorkflowID: &id}))

	_, err = svc.DeleteWorkflow(ctx, connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: id}))
	require.NoError(t, err)

	orphan, err := st.Get(ctx, "chat")
	require.NoError(t, err)
	require.NotNil(t, orphan, "the chat outlives its workflow")
	assert.Nil(t, orphan.WorkflowID, "and is unassigned rather than pointing at a workflow that is gone")

	_, err = svc.DeleteWorkflow(ctx, connect.NewRequest(&workflowv1.DeleteWorkflowRequest{Id: id}))
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err), "deleting it twice is not a silent success")
}

func TestListWorkflowsPages(t *testing.T) {
	svc, _ := workflowService(t)
	ctx := t.Context()

	for _, name := range []string{"one", "two", "three"} {
		_, err := svc.CreateWorkflow(ctx, connect.NewRequest(&workflowv1.CreateWorkflowRequest{Name: name}))
		require.NoError(t, err)
	}

	first, err := svc.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{
		Pagination: &paginationv1.Pagination{Limit: proto.Int32(2)},
	}))
	require.NoError(t, err)
	require.Len(t, first.Msg.GetWorkflows(), 2)
	require.NotEmpty(t, first.Msg.GetNextPageToken(), "a full page offers the next one")

	second, err := svc.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{
		Pagination: &paginationv1.Pagination{Limit: proto.Int32(2), Cursor: proto.String(first.Msg.GetNextPageToken())},
	}))
	require.NoError(t, err)
	require.Len(t, second.Msg.GetWorkflows(), 1)

	seen := map[string]bool{}
	for _, w := range append(first.Msg.GetWorkflows(), second.Msg.GetWorkflows()...) {
		assert.False(t, seen[w.GetId()], "a workflow appears on one page only")
		seen[w.GetId()] = true
	}
	assert.Len(t, seen, 3)
}
