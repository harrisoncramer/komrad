package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHooksComeBackWithEveryReader pins the hooks reaching every shape a workflow is read in,
// which the panel lists, pages, and gets one at a time.
func TestHooksComeBackWithEveryReader(t *testing.T) {
	st := open(t)
	ctx := t.Context()

	hooks := []Hook{
		{Event: HookPreLaunch, Script: "install-deps"},
		{Event: HookPreLaunch, Script: "copy-env"},
		{Event: HookPostLaunch, Script: "announce"},
	}

	created, err := st.Create(ctx, Config{Name: "backend", Hooks: hooks, Worktree: WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)
	assert.Equal(t, hooks, created.Hooks, "create hands back what it wrote")

	got, err := st.Get(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, hooks, got.Hooks, "and so does a get")

	listed, err := st.List(ctx)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, hooks, listed[0].Hooks, "and the listing")

	page, err := st.ListPage(ctx, 10, nil, false)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, hooks, page[0].Hooks, "and a page of it")

	archived, err := st.GetIncludingArchived(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, hooks, archived.Hooks, "and the read that spans the archive")
}
