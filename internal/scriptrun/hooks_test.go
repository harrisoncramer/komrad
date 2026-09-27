package scriptrun

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestHookQueueRunsAnAgentsMomentsInTheOrderTheyWereQueued covers one agent's hooks never overlapping.
func TestHookQueueRunsAnAgentsMomentsInTheOrderTheyWereQueued(t *testing.T) {
	var queue HookQueue

	awaitAuth := func(ctx context.Context) error { return queue.AwaitHooks(ctx, "auth") }
	require.NoError(t, queue.AwaitHooks(t.Context(), "auth"), "nothing is queued yet")

	first := queue.QueueHooks("auth")
	second := queue.QueueHooks("auth")
	other := queue.QueueHooks("billing")

	require.NoError(t, first.Wait(t.Context()), "the first moment waits for nothing")
	require.NoError(t, other.Wait(t.Context()), "another agent's moment waits for nothing")
	require.Error(t, waitBriefly(second.Wait), "the second waits while the first runs")
	require.Error(t, waitBriefly(awaitAuth), "and so does a wait for the whole queue")

	first.Done()
	require.NoError(t, second.Wait(t.Context()), "the second runs once the first has finished")
	require.Error(t, waitBriefly(awaitAuth), "the queue holds while the second runs")

	second.Done()
	require.NoError(t, queue.AwaitHooks(t.Context(), "auth"), "and empties once it has finished")
	other.Done()
}

// TestAHookTurnGivenUpEarlyKeepsItsPlace covers a moment abandoned before its turn came round.
func TestAHookTurnGivenUpEarlyKeepsItsPlace(t *testing.T) {
	var queue HookQueue

	first := queue.QueueHooks("auth")
	abandoned := queue.QueueHooks("auth")
	third := queue.QueueHooks("auth")

	abandoned.Done()
	require.Error(t, waitBriefly(third.Wait), "the third still waits for the first")

	first.Done()
	require.NoError(t, third.Wait(t.Context()))
	third.Done()
	require.NoError(t, queue.AwaitHooks(t.Context(), "auth"))
}

func waitBriefly(wait func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	return wait(ctx)
}
