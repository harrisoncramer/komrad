package mark

import (
	"testing"

	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListByKeyIsTheWatchedSet pins the read-back to the watched set: every mark is held, so a
// letter comes back while it is set and not once it is cleared.
func TestListByKeyIsTheWatchedSet(t *testing.T) {
	ctx := t.Context()
	f := open(t)
	agent := f.seedAgent(t, "agent")
	flow := f.seedWorkflow(t, "flow")

	marked, err := f.Set(ctx, "a", OnAgent(agent))
	require.NoError(t, err)
	require.True(t, marked)
	marked, err = f.Set(ctx, "b", OnWorkflow(flow))
	require.NoError(t, err)
	require.True(t, marked)

	listed, err := f.ListByKey(ctx, []string{"a", "b", "c"})
	require.NoError(t, err)
	assert.Equal(t, []Mark{{Mark: "a", AgentID: &agent}, {Mark: "b", WorkflowID: &flow}}, listed, "a letter nobody holds is left out")

	none, err := f.ListByKey(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, none)

	cleared, err := f.Clear(ctx, OnAgent(agent))
	require.NoError(t, err)
	require.True(t, cleared)

	listed, err = f.ListByKey(ctx, []string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, xslice.Map(listed, func(mark Mark) string { return mark.Mark }), "a cleared letter is a tombstone")

	all, err := f.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, listed, all, "the listing is the whole watched set")
}
