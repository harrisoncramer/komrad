package agents

import (
	"context"
	"testing"

	"claude-agents/internal/store/session"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func agentByID(t *testing.T, st *testStore, id string) session.Agent {
	t.Helper()
	all, err := st.ListAll(context.Background())
	require.NoError(t, err)
	for _, agent := range all {
		if agent.ID == id {
			return agent
		}
	}
	t.Fatalf("no agent with id %q", id)
	return session.Agent{}
}

// TestRestoreRenamesOnNameCollision covers restoring a soft-deleted agent whose
// name was reused by a live agent. The restore must take a fresh unique name and
// leave the live namesake untouched, rather than colliding on the unique-name
// index.
func TestRestoreRenamesOnNameCollision(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")

	require.NoError(t, st.Upsert(ctx, session.Agent{Name: "foo", State: session.StateDone, ClaudeSessionID: "sess-old"}))
	original, err := st.Get(ctx, "foo")
	require.NoError(t, err)
	require.NoError(t, st.SoftDelete(ctx, "foo"))

	// The name is reused by a fresh, live agent.
	require.NoError(t, st.Upsert(ctx, session.Agent{Name: "foo", State: session.StateNew}))
	live, err := st.Get(ctx, "foo")
	require.NoError(t, err)

	toRestore := *original
	toRestore.Deleted = true
	require.NoError(t, manager.restore(ctx, toRestore), "restore must not collide on the reused name")

	restored := agentByID(t, st, original.ID)
	assert.False(t, restored.Deleted, "the restored row is undeleted")
	assert.NotEqual(t, "foo", restored.Name, "the restored agent takes a fresh name")
	assert.Equal(t, session.StateNew, restored.State)
	assert.Equal(t, "sess-old", restored.ClaudeSessionID, "the restored conversation is preserved")

	stillLive, err := st.Get(ctx, "foo")
	require.NoError(t, err)
	require.NotNil(t, stillLive)
	assert.Equal(t, live.ID, stillLive.ID, "the live namesake is untouched")
	assert.Equal(t, session.StateNew, stillLive.State)
}

// TestRestoreKeepsNameWhenFree confirms the common restore path, where no live
// agent holds the name, keeps the original name and id.
func TestRestoreKeepsNameWhenFree(t *testing.T) {
	ctx := context.Background()
	st := newStore(t)
	manager := newManager(newFakeTmux(), st, "")

	require.NoError(t, st.Upsert(ctx, session.Agent{Name: "solo", State: session.StateDone, ClaudeSessionID: "sess-1"}))
	original, err := st.Get(ctx, "solo")
	require.NoError(t, err)
	require.NoError(t, st.SoftDelete(ctx, "solo"))

	toRestore := *original
	toRestore.Deleted = true
	require.NoError(t, manager.restore(ctx, toRestore))

	got, err := st.Get(ctx, "solo")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, original.ID, got.ID, "restore keeps the id")
	assert.Equal(t, session.StateNew, got.State)
	assert.Equal(t, "sess-1", got.ClaudeSessionID)
}
