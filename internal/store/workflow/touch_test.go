package workflow

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkflowWritesTouchTheWorkflow pins that a create, a rename and an archive each name the
// workflow.
func TestWorkflowWritesTouchTheWorkflow(t *testing.T) {
	ctx := t.Context()
	st, touched := openWatched(t)

	created, err := st.Create(ctx, Config{Name: "flow", Worktree: WorktreeConfig{BaseBranch: "main"}})
	require.NoError(t, err)
	assert.Equal(t, []string{created.ID}, slices.Compact(touched.Workflows()), "a create names the new workflow")

	touched.Reset()
	require.NoError(t, st.Rename(ctx, created.ID, "renamed"))
	assert.Equal(t, []string{created.ID}, touched.Workflows())

	touched.Reset()
	require.NoError(t, st.Update(ctx, created.ID, Config{Name: "renamed", Worktree: WorktreeConfig{BaseBranch: "develop"}}))
	assert.Equal(t, []string{created.ID}, slices.Compact(touched.Workflows()), "an update names the workflow")
	assert.Empty(t, touched.Agents(), "a workflow with no agents names none")

	touched.Reset()
	require.NoError(t, st.Archive(ctx, created.ID))
	assert.Equal(t, []string{created.ID}, touched.Workflows())
}
