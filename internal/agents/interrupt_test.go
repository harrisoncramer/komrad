package agents

import (
	"errors"
	"testing"

	"weave/internal/harness"
	"weave/internal/harnesstest"
	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// interrupterOver builds an interrupter over the test store.
func interrupterOver(st *testStore, channel *harnesstest.Channel, observer harness.Observer) *Interrupter {
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

	channel := &harnesstest.Channel{}
	observer := &settlingObserver{readings: []harness.Activity{harness.ActivityIdle}}

	require.NoError(t, interrupterOver(st, channel, observer).Interrupt(t.Context(), agent.ID))

	assert.Zero(t, channel.Interrupts, "there was no turn to cut")
	assert.Equal(t, 1, observer.reads, "and the pane was read to find that out")
}

// TestInterruptLeavesADraftAlone is the other half of rest: a person part-way through typing has
// nothing running either.
func TestInterruptLeavesADraftAlone(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "sextant", "%1", agentstore.StateIdle)

	channel := &harnesstest.Channel{}
	observer := &settlingObserver{readings: []harness.Activity{harness.ActivityComposing}}

	require.NoError(t, interrupterOver(st, channel, observer).Interrupt(t.Context(), agent.ID))

	assert.Zero(t, channel.Interrupts)
}

// TestInterruptStopsATurnInFlight is the case the keys were ever sent for, a prompt waiting on an
// answer counting as a turn since that is the frame the keys are meant to clear.
func TestInterruptStopsATurnInFlight(t *testing.T) {
	for name, activity := range map[string]harness.Activity{
		"working": harness.ActivityWorking,
		"waiting": harness.ActivityWaiting,
	} {
		t.Run(name, func(t *testing.T) {
			st := newStore(t)
			agent := seed(t, st, "compass", "%1", agentstore.StateThinking)

			channel := &harnesstest.Channel{}
			observer := &settlingObserver{readings: []harness.Activity{activity}}

			require.NoError(t, interrupterOver(st, channel, observer).Interrupt(t.Context(), agent.ID))

			assert.Equal(t, 1, channel.Interrupts)
		})
	}
}

// TestInterruptLeavesAnUnrecognizedFrameAlone is the reading that is not evidence of a turn: the
// pane was captured and matched none of the harness's patterns, so an agent sitting at rest in a
// frame weave cannot read would take the keys in the one case nobody would see it happen.
func TestInterruptLeavesAnUnrecognizedFrameAlone(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "quadrant", "%1", agentstore.StateThinking)

	channel := &harnesstest.Channel{}
	observer := &settlingObserver{readings: []harness.Activity{harness.ActivityUnrecognized}}

	require.NoError(t, interrupterOver(st, channel, observer).Interrupt(t.Context(), agent.ID))

	assert.Zero(t, channel.Interrupts)
}

// TestInterruptStopsAnUnreadablePane is the direction the doubt falls in: a capture that failed
// outright is no evidence either way, and the keys are what a rebase sends before it moves files
// under a running agent.
func TestInterruptStopsAnUnreadablePane(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "astrolabe", "%1", agentstore.StateThinking)

	channel := &harnesstest.Channel{}
	observer := &settlingObserver{err: errors.New("no pane")}

	require.NoError(t, interrupterOver(st, channel, observer).Interrupt(t.Context(), agent.ID))

	assert.Equal(t, 1, channel.Interrupts)
}
