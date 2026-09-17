package agents

import (
	"errors"
	"testing"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// interrupterOver builds an interrupter over the test store.
func interrupterOver(st *testStore, channel *fakeChannel, observer harness.Observer) *Interrupter {
	return NewInterrupter(NewInterrupterParams{
		Agents:   st.Agents,
		Channel:  channel,
		Observer: observer,
	})
}

// TestInterruptLeavesAnAgentAtRestAlone is what the Rewind dialog cost: the keys that stop Claude
// mid-turn are the ones that open its checkpoint picker when there is no turn to stop, and the
// dialog then refuses every later delivery.
func TestInterruptLeavesAnAgentAtRestAlone(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	channel := &fakeChannel{}
	observer := &settlingObserver{readings: []harness.Activity{harness.ActivityIdle}}

	require.NoError(t, interrupterOver(st, channel, observer).Interrupt(t.Context(), agent.ID))

	assert.Zero(t, channel.interrupts, "there was no turn to cut")
	assert.Equal(t, 1, observer.reads, "and the pane was read to find that out")
}

// TestInterruptLeavesADraftAlone is the other half of rest: a person part-way through typing has
// nothing running either.
func TestInterruptLeavesADraftAlone(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "sextant", "%1", agentstore.StateIdle)

	channel := &fakeChannel{}
	observer := &settlingObserver{readings: []harness.Activity{harness.ActivityComposing}}

	require.NoError(t, interrupterOver(st, channel, observer).Interrupt(t.Context(), agent.ID))

	assert.Zero(t, channel.interrupts)
}

// TestInterruptStopsAWorkingAgent is the case the keys were ever sent for.
func TestInterruptStopsAWorkingAgent(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "compass", "%1", agentstore.StateThinking)

	channel := &fakeChannel{}
	observer := &settlingObserver{readings: []harness.Activity{harness.ActivityWorking}}

	require.NoError(t, interrupterOver(st, channel, observer).Interrupt(t.Context(), agent.ID))

	assert.Equal(t, 1, channel.interrupts)
}

// TestInterruptStopsAnUnreadablePane is the direction the doubt falls in: a pane nothing could
// read is likelier mid-turn than at rest, and the keys are what a rebase sends before it moves
// files under a running agent.
func TestInterruptStopsAnUnreadablePane(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "astrolabe", "%1", agentstore.StateThinking)

	channel := &fakeChannel{}
	observer := &settlingObserver{err: errors.New("no pane")}

	require.NoError(t, interrupterOver(st, channel, observer).Interrupt(t.Context(), agent.ID))

	assert.Equal(t, 1, channel.interrupts)
}
