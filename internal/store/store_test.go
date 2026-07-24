package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func open(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "agents.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	return st
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "colon becomes a dash",
			in:   "a:b",
			want: "a-b",
		},
		{
			name: "dot becomes a dash",
			in:   "v1.0",
			want: "v1-0",
		},
		{
			name: "space becomes a dash",
			in:   "my agent",
			want: "my-agent",
		},
		{
			name: "a mix is fully sanitized",
			in:   "My Agent: v1.0",
			want: "My-Agent--v1-0",
		},
		{
			name: "an already-clean name is unchanged",
			in:   "clean-name",
			want: "clean-name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SanitizeName(tt.in))
		})
	}
}

// TestReuseNameAfterSoftDelete checks that a soft-deleted row keeps its name and
// reusing that name creates a fresh row rather than resurrecting the dead one.
// State events keyed by name must land on the live row.
func TestReuseNameAfterSoftDelete(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	require.NoError(t, st.Upsert(ctx, Agent{Name: "agent", Pane: "%1", State: StateThinking}))
	first, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, first)

	require.NoError(t, st.SoftDelete(ctx, "agent"))
	got, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	assert.Nil(t, got, "a soft-deleted row is hidden from Get")

	require.NoError(t, st.Upsert(ctx, Agent{Name: "agent", Pane: "%2", State: StateNew}))
	second, err := st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, second)

	assert.NotEqual(t, first.ID, second.ID, "reused name must not resurrect the deleted row")
	assert.Equal(t, StateNew, second.State)
	assert.Equal(t, "%2", second.Pane)

	live, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, live, 1)
	assert.Equal(t, second.ID, live[0].ID)

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2, "both the live and deleted rows are retained")

	// A state event keyed by name updates the live row, leaving the deleted row's
	// state frozen at what it was when deleted.
	require.NoError(t, st.UpdateState(ctx, "agent", "%2", StateWaiting, ""))
	got, err = st.Get(ctx, "agent")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, second.ID, got.ID)
	assert.Equal(t, StateWaiting, got.State)

	for _, agent := range all {
		if agent.ID == first.ID {
			assert.Equal(t, StateThinking, agent.State, "the deleted row must not be mutated")
		}
	}
}

// TestUpdateStateDoesNotInsert guards the rule that UpdateState never creates a
// row. A state write for a session the tool did not create finds no row and is a
// no-op, so an outside Claude session never leaks into the store.
func TestUpdateStateDoesNotInsert(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	require.NoError(t, st.UpdateState(ctx, "external", "%5", StateThinking, "sess"))

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	assert.Empty(t, all, "an update for an unknown session must not insert a row")
}

func TestActiveNameExists(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	exists, err := st.ActiveNameExists(ctx, "agent")
	require.NoError(t, err)
	assert.False(t, exists, "no agent yet")

	require.NoError(t, st.Upsert(ctx, Agent{Name: "agent", State: StateNew}))
	exists, err = st.ActiveNameExists(ctx, "agent")
	require.NoError(t, err)
	assert.True(t, exists, "a live agent holds the name")

	require.NoError(t, st.SoftDelete(ctx, "agent"))
	exists, err = st.ActiveNameExists(ctx, "agent")
	require.NoError(t, err)
	assert.False(t, exists, "only a deleted agent holds the name")
}

// TestDeleteThenRestore mirrors the manager's restore ordering at the store
// layer: undelete by id, then upsert the same id. It must not collide on the
// primary key, and it must leave exactly one live row keyed to the same id and
// conversation.
func TestDeleteThenRestore(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	require.NoError(t, st.Upsert(ctx, Agent{Name: "beta", State: StateThinking, ClaudeSessionID: "sess-1"}))
	original, err := st.Get(ctx, "beta")
	require.NoError(t, err)
	require.NotNil(t, original)
	require.NoError(t, st.SoftDelete(ctx, "beta"))

	require.NoError(t, st.Undelete(ctx, original.ID))
	restored := *original
	restored.State = StateNew
	restored.Pane = "%9"
	require.NoError(t, st.Upsert(ctx, restored), "upsert on restore must not collide")

	got, err := st.Get(ctx, "beta")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, original.ID, got.ID, "restored row keeps its id")
	assert.Equal(t, StateNew, got.State)
	assert.Equal(t, "sess-1", got.ClaudeSessionID, "restore preserves the conversation")

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 1, "a single row after restore")
}

// TestWorkflowCRUD covers the workflow lifecycle: create, list newest-first, get
// by id, rename by id, and soft-delete hiding it from both list and get.
func TestWorkflowCRUD(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	first, err := st.CreateWorkflow(ctx, WorkflowConfig{Name: "backend"})
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.NotEmpty(t, first.ID, "an id is generated on create")

	second, err := st.CreateWorkflow(ctx, WorkflowConfig{Name: "frontend"})
	require.NoError(t, err)
	require.NotNil(t, second)

	workflows, err := st.ListWorkflows(ctx)
	require.NoError(t, err)
	require.Len(t, workflows, 2)
	names := []string{workflows[0].Name, workflows[1].Name}
	assert.ElementsMatch(t, []string{"backend", "frontend"}, names)

	got, err := st.GetWorkflow(ctx, first.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "backend", got.Name)

	require.NoError(t, st.RenameWorkflow(ctx, first.ID, "platform"))
	got, err = st.GetWorkflow(ctx, first.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "platform", got.Name)

	require.NoError(t, st.DeleteWorkflow(ctx, first.ID))
	got, err = st.GetWorkflow(ctx, first.ID)
	require.NoError(t, err)
	assert.Nil(t, got, "a deleted workflow is hidden from Get")

	workflows, err = st.ListWorkflows(ctx)
	require.NoError(t, err)
	require.Len(t, workflows, 1)
	assert.Equal(t, second.ID, workflows[0].ID)
}

// TestSessionConfigRoundTrip checks that a session's cwd and flags round-trip
// through session_configs. An insert creates the owned config row, a read joins
// it back, and an id-keyed update rewrites the same row rather than orphaning it.
func TestSessionConfigRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	require.NoError(t, st.Upsert(ctx, Agent{Name: "chat", Cwd: "/work/api", Flags: "--model opus", State: StateNew}))

	got, err := st.Get(ctx, "chat")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "/work/api", got.Cwd)
	assert.Equal(t, "--model opus", got.Flags)
	assert.NotEmpty(t, got.SessionConfigID, "a session owns its config row")

	got.Cwd = "/work/web"
	got.Pane = "%3"
	require.NoError(t, st.Upsert(ctx, *got))

	again, err := st.Get(ctx, "chat")
	require.NoError(t, err)
	require.NotNil(t, again)
	assert.Equal(t, got.SessionConfigID, again.SessionConfigID, "the config id is stable across updates")
	assert.Equal(t, "/work/web", again.Cwd, "the linked config is updated in place")
	assert.Equal(t, "%3", again.Pane)
}

// TestWorkflowConfigRoundTrip checks that a workflow's default cwd/flags are
// stored in workflow_configs and joined back on both the create result and the
// later reads.
func TestWorkflowConfigRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	wf, err := st.CreateWorkflow(ctx, WorkflowConfig{Name: "backend", Context: "shared notes", Cwd: "/work/api", Flags: "--model opus"})
	require.NoError(t, err)
	assert.Equal(t, "shared notes", wf.Context)
	assert.Equal(t, "/work/api", wf.Cwd)
	assert.Equal(t, "--model opus", wf.Flags)

	got, err := st.GetWorkflow(ctx, wf.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "shared notes", got.Context)
	assert.Equal(t, "/work/api", got.Cwd)
	assert.Equal(t, "--model opus", got.Flags)

	list, err := st.ListWorkflows(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "shared notes", list[0].Context)
	assert.Equal(t, "/work/api", list[0].Cwd)
	assert.Equal(t, "--model opus", list[0].Flags)

	require.NoError(t, st.UpdateWorkflow(ctx, wf.ID, WorkflowConfig{Name: "backend", Context: "revised notes", Cwd: "/work/api", Flags: "--model opus"}))
	after, err := st.GetWorkflow(ctx, wf.ID)
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, "revised notes", after.Context)
}

// TestMoveChatBetweenWorkflows checks that assigning a chat to a workflow scopes
// it to that workflow's listing, and that a nil target unassigns it back to the
// root.
func TestMoveChatBetweenWorkflows(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	workflow, err := st.CreateWorkflow(ctx, WorkflowConfig{Name: "backend"})
	require.NoError(t, err)
	require.NoError(t, st.Upsert(ctx, Agent{Name: "scoped", State: StateNew}))
	require.NoError(t, st.Upsert(ctx, Agent{Name: "unscoped", State: StateNew}))

	scoped, err := st.Get(ctx, "scoped")
	require.NoError(t, err)
	require.NotNil(t, scoped)
	assert.Nil(t, scoped.WorkflowID, "a new chat is unassigned")

	require.NoError(t, st.MoveToWorkflow(ctx, scoped.ID, &workflow.ID))

	moved, err := st.Get(ctx, "scoped")
	require.NoError(t, err)
	require.NotNil(t, moved.WorkflowID)
	assert.Equal(t, workflow.ID, *moved.WorkflowID)

	all, err := st.List(ctx)
	require.NoError(t, err)
	assert.Len(t, all, 2, "the root list still shows every chat regardless of workflow")

	require.NoError(t, st.MoveToWorkflow(ctx, scoped.ID, nil))
	unassigned, err := st.Get(ctx, "scoped")
	require.NoError(t, err)
	assert.Nil(t, unassigned.WorkflowID, "a nil target unassigns the chat")
}

// TestDeleteWorkflowOrphansChats is the orphan-on-delete guarantee: deleting a
// workflow returns its chats to unassigned rather than stranding them behind a
// hidden workflow.
func TestDeleteWorkflowOrphansChats(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	workflow, err := st.CreateWorkflow(ctx, WorkflowConfig{Name: "backend"})
	require.NoError(t, err)
	require.NoError(t, st.Upsert(ctx, Agent{Name: "chat", State: StateNew, WorkflowID: &workflow.ID}))

	require.NoError(t, st.DeleteWorkflow(ctx, workflow.ID))

	got, err := st.Get(ctx, "chat")
	require.NoError(t, err)
	require.NotNil(t, got, "the chat survives its workflow's deletion")
	assert.Nil(t, got.WorkflowID, "the chat is orphaned back to unassigned")
}

// TestDeleteWorkflowWithChats is the purge path: the workflow and its chats are
// soft-deleted together, so they leave the live listing but remain findable in
// the all-inclusive search listing.
func TestDeleteWorkflowWithChats(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	workflow, err := st.CreateWorkflow(ctx, WorkflowConfig{Name: "backend"})
	require.NoError(t, err)
	require.NoError(t, st.Upsert(ctx, Agent{Name: "chat", State: StateNew, WorkflowID: &workflow.ID}))

	require.NoError(t, st.DeleteWorkflowWithChats(ctx, workflow.ID))

	got, err := st.Get(ctx, "chat")
	require.NoError(t, err)
	assert.Nil(t, got, "the chat is hidden from the live listing")

	all, err := st.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1, "the soft-deleted chat is retained for search")
	assert.True(t, all[0].Deleted)
	require.NotNil(t, all[0].WorkflowID, "the chat keeps its workflow id while deleted")
	assert.Equal(t, workflow.ID, *all[0].WorkflowID)

	workflows, err := st.ListWorkflows(ctx)
	require.NoError(t, err)
	assert.Empty(t, workflows, "the workflow is hidden too")
}

// TestUpsertPreservesWorkflow mirrors the manager's restore, which round-trips
// the whole agent through Upsert. A restored chat must keep the workflow it
// belonged to.
func TestUpsertPreservesWorkflow(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	workflow, err := st.CreateWorkflow(ctx, WorkflowConfig{Name: "backend"})
	require.NoError(t, err)
	require.NoError(t, st.Upsert(ctx, Agent{Name: "chat", State: StateThinking, WorkflowID: &workflow.ID}))

	original, err := st.Get(ctx, "chat")
	require.NoError(t, err)
	require.NotNil(t, original)
	require.NoError(t, st.SoftDelete(ctx, "chat"))

	require.NoError(t, st.Undelete(ctx, original.ID))
	restored := *original
	restored.State = StateNew
	require.NoError(t, st.Upsert(ctx, restored))

	got, err := st.Get(ctx, "chat")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.WorkflowID, "restore preserves the workflow")
	assert.Equal(t, workflow.ID, *got.WorkflowID)
}

func TestStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	require.NoError(t, st.Upsert(ctx, Agent{
		Name:   "alpha",
		Pane:   "%1",
		Cwd:    "/tmp",
		Flags:  "--model opus",
		Prompt: "do the thing",
		State:  StateNew,
	}))

	agents, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, agents, 1)
	assert.NotEmpty(t, agents[0].ID, "an id is generated on insert")
	assert.Equal(t, "/tmp", agents[0].Cwd)
	assert.Equal(t, "--model opus", agents[0].Flags)

	require.NoError(t, st.SetState(ctx, "alpha", StateDone))
	require.NoError(t, st.Delete(ctx, "alpha"))

	agents, err = st.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, agents, "a hard delete drops the row")
}
