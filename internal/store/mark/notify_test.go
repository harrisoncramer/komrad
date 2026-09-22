package mark

import (
	"testing"

	"weave/internal/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMarksNotifyBothEndsOfAMove pins that setting a letter names the row that took it, and
// that clearing the conflicts names the rows the letter and the target were freed from.
func TestMarksNotifyBothEndsOfAMove(t *testing.T) {
	ctx := t.Context()
	f := open(t)
	first := f.seedAgent(t, "first")
	second := f.seedAgent(t, "second")
	flow := f.seedWorkflow(t, "flow")
	raised := dbtest.Listen(t, f.url)

	marked, err := f.Set(ctx, "a", OnAgent(first))
	require.NoError(t, err)
	require.True(t, marked)
	assert.Equal(t, "agents "+first, raised.Next(t), "a set names the row that took the letter")

	marked, err = f.Set(ctx, "b", OnWorkflow(flow))
	require.NoError(t, err)
	require.True(t, marked)
	assert.Equal(t, "workflows "+flow, raised.Next(t))

	require.NoError(t, f.ClearConflicts(ctx, "a", OnAgent(second)))
	assert.Equal(t, "agents "+first, raised.Next(t), "clearing the conflicts names the row that lost the letter")

	cleared, err := f.Clear(ctx, OnWorkflow(flow))
	require.NoError(t, err)
	require.True(t, cleared)
	assert.Equal(t, "workflows "+flow, raised.Next(t))
}
