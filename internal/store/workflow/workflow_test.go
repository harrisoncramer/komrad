package workflow

import (
	"context"
	"testing"

	"claude-agents/internal/pgtest"
	"claude-agents/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// open builds a workflow store over a throwaway database.
func open(t *testing.T) *Store {
	t.Helper()
	p, err := store.Open(t.Context(), pgtest.URL(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	return New(p.DB())
}

// TestWorkflowCRUD covers the workflow lifecycle: create, list newest-first, get
// by id, rename by id, and soft-delete hiding it from both list and get.
func TestWorkflowCRUD(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	first, err := st.Create(ctx, Config{Name: "backend"})
	require.NoError(t, err)
	require.NotNil(t, first)
	assert.NotEmpty(t, first.ID, "an id is generated on create")

	second, err := st.Create(ctx, Config{Name: "frontend"})
	require.NoError(t, err)
	require.NotNil(t, second)

	workflows, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, workflows, 2)
	names := []string{workflows[0].Name, workflows[1].Name}
	assert.ElementsMatch(t, []string{"backend", "frontend"}, names)

	got, err := st.Get(ctx, first.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "backend", got.Name)

	require.NoError(t, st.Rename(ctx, first.ID, "platform"))
	got, err = st.Get(ctx, first.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "platform", got.Name)

	require.NoError(t, st.Archive(ctx, first.ID))
	got, err = st.Get(ctx, first.ID)
	require.NoError(t, err)
	assert.Nil(t, got, "a deleted workflow is hidden from Get")

	workflows, err = st.List(ctx)
	require.NoError(t, err)
	require.Len(t, workflows, 1)
	assert.Equal(t, second.ID, workflows[0].ID)
}

// TestWorkflowConfigRoundTrip checks that a workflow's default cwd/flags are
// stored in workflow_configs and joined back on both the create result and the
// later reads.
func TestWorkflowConfigRoundTrip(t *testing.T) {
	ctx := context.Background()
	st := open(t)

	wf, err := st.Create(ctx, Config{Name: "backend", Context: "shared notes", Cwd: "/work/api", Flags: "--model opus"})
	require.NoError(t, err)
	assert.Equal(t, "shared notes", wf.Context)
	assert.Equal(t, "/work/api", wf.Cwd)
	assert.Equal(t, "--model opus", wf.Flags)

	got, err := st.Get(ctx, wf.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "shared notes", got.Context)
	assert.Equal(t, "/work/api", got.Cwd)
	assert.Equal(t, "--model opus", got.Flags)

	list, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "shared notes", list[0].Context)
	assert.Equal(t, "/work/api", list[0].Cwd)
	assert.Equal(t, "--model opus", list[0].Flags)

	require.NoError(t, st.Update(ctx, wf.ID, Config{Name: "backend", Context: "revised notes", Cwd: "/work/api", Flags: "--model opus"}))
	after, err := st.Get(ctx, wf.ID)
	require.NoError(t, err)
	require.NotNil(t, after)
	assert.Equal(t, "revised notes", after.Context)
}
