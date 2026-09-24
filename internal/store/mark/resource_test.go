package mark

import (
	"slices"
	"testing"

	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListWatchedIsTheWatchedSet pins the read-back to the watched set: every mark is held, so a
// mark comes back while it is set and not once it is cleared.
func TestListWatchedIsTheWatchedSet(t *testing.T) {
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

	all, err := f.ListWatched(ctx, nil)
	require.NoError(t, err)
	require.Len(t, all, 2)
	ids := xslice.Map(all, func(mark Mark) string { return mark.ID })

	listed, err := f.ListWatched(ctx, append(slices.Clone(ids), "00000000-0000-0000-0000-000000000000", "not-an-id"))
	require.NoError(t, err)
	assert.Equal(t, []Mark{
		{
			ID:      ids[0],
			Mark:    "a",
			AgentID: &agent,
		},
		{
			ID:         ids[1],
			Mark:       "b",
			WorkflowID: &flow,
		},
	}, listed, "a key nobody holds is left out")

	none, err := f.ListWatched(ctx, []string{})
	require.NoError(t, err)
	assert.Empty(t, none, "an empty key list reads nothing")

	cleared, err := f.Clear(ctx, OnAgent(agent))
	require.NoError(t, err)
	require.True(t, cleared)

	listed, err = f.ListWatched(ctx, ids)
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, xslice.Map(listed, func(mark Mark) string { return mark.Mark }), "a cleared mark is a tombstone")

	all, err = f.ListWatched(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, listed, all, "the snapshot is the whole watched set")
}
