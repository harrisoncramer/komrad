package scriptrun

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// TestHookQueueRunsAnAgentsMomentsInTheOrderTheyWereQueued covers one agent's hooks never overlapping.
func TestHookQueueRunsAnAgentsMomentsInTheOrderTheyWereQueued(t *testing.T) {
	var queue HookQueue

	awaitAuth := func(ctx context.Context) error { return queue.AwaitHooks(ctx, authAgentID) }
	require.NoError(t, queue.AwaitHooks(t.Context(), authAgentID), "nothing is queued yet")

	first := queue.QueueHooks(authAgentID)
	second := queue.QueueHooks(authAgentID)
	other := queue.QueueHooks(uuid.New())

	require.NoError(t, first.Wait(t.Context()), "the first moment waits for nothing")
	require.NoError(t, other.Wait(t.Context()), "another agent's moment waits for nothing")
	require.Error(t, waitBriefly(t.Context(), second.Wait), "the second waits while the first runs")
	require.Error(t, waitBriefly(t.Context(), awaitAuth), "and so does a wait for the whole queue")

	first.Done()
	require.NoError(t, second.Wait(t.Context()), "the second runs once the first has finished")
	require.Error(t, waitBriefly(t.Context(), awaitAuth), "the queue holds while the second runs")

	second.Done()
	require.NoError(t, queue.AwaitHooks(t.Context(), authAgentID), "and empties once it has finished")
	other.Done()
}

// TestAHookTurnGivenUpEarlyKeepsItsPlace covers a moment abandoned before its turn came round.
func TestAHookTurnGivenUpEarlyKeepsItsPlace(t *testing.T) {
	var queue HookQueue

	first := queue.QueueHooks(authAgentID)
	abandoned := queue.QueueHooks(authAgentID)
	third := queue.QueueHooks(authAgentID)

	abandoned.Done()
	require.Error(t, waitBriefly(t.Context(), third.Wait), "the third still waits for the first")

	first.Done()
	require.NoError(t, third.Wait(t.Context()))
	third.Done()
	require.NoError(t, queue.AwaitHooks(t.Context(), authAgentID))
}

func waitBriefly(ctx context.Context, wait func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()

	return wait(ctx)
}

var (
	authAgentID = uuid.New()
)
