package mark

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarksTouchBothEndsOfAMove pins that setting a letter names the row that took it, and
// that clearing the conflicts names the rows the letter and the target were freed from.
func TestMarksTouchBothEndsOfAMove(t *testing.T) {
	ctx := t.Context()
	f := open(t)
	first := f.seedAgent(t, "first")
	second := f.seedAgent(t, "second")
	flow := f.seedWorkflow(t, "flow")
	f.touched.Reset()

	marked, err := f.Set(ctx, "a", OnAgent(first))
	require.NoError(t, err)
	require.True(t, marked)
	assert.Equal(t, []string{first}, f.touched.Agents(), "a set names the row that took the letter")

	marked, err = f.Set(ctx, "b", OnWorkflow(flow))
	require.NoError(t, err)
	require.True(t, marked)
	assert.Equal(t, []string{flow}, f.touched.Workflows())

	f.touched.Reset()
	require.NoError(t, f.ClearConflicts(ctx, "a", OnAgent(second)))
	assert.Equal(t, []string{first}, f.touched.Agents(), "clearing the conflicts names the row that lost the letter")

	f.touched.Reset()
	cleared, err := f.Clear(ctx, OnWorkflow(flow))
	require.NoError(t, err)
	require.True(t, cleared)
	assert.Equal(t, []string{flow}, f.touched.Workflows())
}
