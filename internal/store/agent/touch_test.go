package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWritesTouchTheWatch pins that a write names the agent it changed to the watch: a create,
// a state change, an archive, and a delete claim that takes a child and the archived workflow
// left empty with it.
func TestWritesTouchTheWatch(t *testing.T) {
	ctx := t.Context()
	f := open(t)

	require.NoError(t, f.Create(ctx, Agent{Name: "parent", Cwd: "/repo", State: StateIdle}))
	parent, err := f.Get(ctx, "parent")
	require.NoError(t, err)
	assert.Equal(t, []string{parent.ID}, f.touched.Agents(), "a create names the new row")

	f.touched.Reset()
	require.NoError(t, f.SetStateByID(ctx, parent.ID, StateThinking))
	assert.Equal(t, []string{parent.ID}, f.touched.Agents(), "a state change names the row")

	require.NoError(t, f.Create(ctx, Agent{Name: "child", Cwd: "/repo", State: StateIdle, ParentAgentID: &parent.ID}))
	child, err := f.Get(ctx, "child")
	require.NoError(t, err)

	f.touched.Reset()
	_, err = f.Archive(ctx, []string{parent.ID, child.ID})
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{parent.ID, child.ID}, f.touched.Agents(), "an archive names every row it moved")
	require.NoError(t, f.workflows.Archive(ctx, f.defaultWorkflow))

	f.touched.Reset()
	require.NoError(t, f.MarkDeleting(ctx, []string{parent.ID}))
	assert.ElementsMatch(t, []string{parent.ID, child.ID}, f.touched.Agents(), "a delete claim names the child it takes too")
	assert.Equal(t, []string{f.defaultWorkflow}, f.touched.Workflows(), "and the archived workflow the claim empties")
}
