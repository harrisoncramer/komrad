package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	"weave/internal/harness"
	agentstore "weave/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// settlingObserver answers with a run of readings, holding the last one once they are spent, which
// is how a pane that takes a moment to come to rest is written down.
type settlingObserver struct {
	readings []harness.Activity
	err      error
	reads    int
}

func (o *settlingObserver) Observe(_ context.Context, _ harness.Target) (harness.Observation, error) {
	if o.err != nil {
		return harness.Observation{}, o.err
	}

	at := min(o.reads, len(o.readings)-1)
	o.reads++

	return harness.Observation{Activity: o.readings[at]}, nil
}

// holderOver builds a holder over the test store with a clock the test moves itself, so waiting
// for an agent to settle costs no real time.
func holderOver(st *testStore, channel *fakeChannel, observer harness.Observer) *Holder {
	at := time.Now()

	return NewHolder(NewHolderParams{
		Agents:   st.Agents,
		Channel:  channel,
		Observer: observer,
		Clock: func() time.Time {
			at = at.Add(holdSettleInterval)

			return at
		},
		// The budget is walked by the clock above, so resting between readings costs no real time.
		Wait: func(time.Duration) <-chan time.Time {
			rested := make(chan time.Time, 1)
			rested <- at

			return rested
		},
	})
}

// TestHoldWaitsForTheAgentToStop is the whole safety argument: the interrupt is keys typed into a
// terminal, so what may claim the worktree is an agent observed to have come to rest.
func TestHoldWaitsForTheAgentToStop(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateThinking)

	channel := &fakeChannel{}
	observer := &settlingObserver{readings: []harness.Activity{
		harness.ActivityWorking,
		harness.ActivityWorking,
		harness.ActivityIdle,
	}}

	held, err := holderOver(st, channel, observer).Hold(t.Context(), agent.ID)
	require.NoError(t, err)

	assert.NotNil(t, held.HeldAt, "the hold is written down only once the agent has stopped")
	assert.True(t, held.HeldMidTurn, "and it says the turn it cut was still running")
	assert.Equal(t, 1, channel.interrupts)
	assert.Equal(t, 3, observer.reads, "the pane was read until it settled")
}

// TestHoldRefusesAnAgentThatWillNotStop covers the interrupt that never lands, where claiming the
// worktree would put a person's editor on top of a writer.
func TestHoldRefusesAnAgentThatWillNotStop(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateThinking)

	observer := &settlingObserver{readings: []harness.Activity{harness.ActivityWorking}}

	_, err := holderOver(st, &fakeChannel{}, observer).Hold(t.Context(), agent.ID)
	require.ErrorIs(t, err, ErrHoldRefused)
	assert.Contains(t, err.Error(), "had not stopped")

	after := agentByID(t, st, agent.ID)
	assert.Nil(t, after.HeldAt, "a refused hold claims nothing")
}

// TestHoldRefusesAPaneItCannotRead pins the state that means the reading failed rather than that
// the agent is idle, which is the one case nothing can call safe.
func TestHoldRefusesAPaneItCannotRead(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateThinking)

	observer := &settlingObserver{err: errors.New("the pane is gone")}

	_, err := holderOver(st, &fakeChannel{}, observer).Hold(t.Context(), agent.ID)
	require.ErrorIs(t, err, ErrHoldRefused)
	assert.Contains(t, err.Error(), "could not be read")

	after := agentByID(t, st, agent.ID)
	assert.Nil(t, after.HeldAt)
}

// TestHoldingAnIdleAgentTypesNothingAtIt covers holding an agent that was already at rest, which
// is reading rather than interrupting.
func TestHoldingAnIdleAgentTypesNothingAtIt(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	channel := &fakeChannel{}
	observer := &settlingObserver{readings: []harness.Activity{harness.ActivityIdle}}

	held, err := holderOver(st, channel, observer).Hold(t.Context(), agent.ID)
	require.NoError(t, err)

	assert.Zero(t, channel.interrupts, "there was no turn to cut")
	assert.False(t, held.HeldMidTurn)
}

// TestReleasingATurnThatWasCutSaysWhatChanged is what makes an agent's next turn start from the
// truth rather than from what it remembers.
func TestReleasingATurnThatWasCutSaysWhatChanged(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateThinking)

	channel := &fakeChannel{}
	holder := holderOver(st, channel, &settlingObserver{readings: []harness.Activity{
		harness.ActivityWorking,
		harness.ActivityIdle,
	}})

	_, err := holder.Hold(t.Context(), agent.ID)
	require.NoError(t, err)

	released, err := holder.Release(t.Context(), ReleaseParams{
		AgentID: agent.ID,
		Said:    "I changed review.go while you were held",
	})
	require.NoError(t, err)

	assert.Nil(t, released.HeldAt, "nobody is holding it now")
	assert.Equal(t, []string{"I changed review.go while you were held"}, channel.sent)
}

// TestReleasingAnAgentThatHadFinishedTypesNothing covers the agent whose own turn ended before the
// hold, which typing at would put back to work nobody asked for.
func TestReleasingAnAgentThatHadFinishedTypesNothing(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	channel := &fakeChannel{}
	holder := holderOver(st, channel, &settlingObserver{readings: []harness.Activity{harness.ActivityIdle}})

	_, err := holder.Hold(t.Context(), agent.ID)
	require.NoError(t, err)

	released, err := holder.Release(t.Context(), ReleaseParams{
		AgentID: agent.ID,
		Said:    "I changed review.go while you were held",
	})
	require.NoError(t, err)

	assert.Nil(t, released.HeldAt)
	assert.Empty(t, channel.sent, "there was no cut turn to carry on with")
}

// TestHoldingAnAgentTwiceIsRefused pins the second person, or the second keypress, finding it
// already taken rather than quietly moving the stamp.
func TestHoldingAnAgentTwiceIsRefused(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	holder := holderOver(st, &fakeChannel{}, &settlingObserver{readings: []harness.Activity{harness.ActivityIdle}})

	first, err := holder.Hold(t.Context(), agent.ID)
	require.NoError(t, err)

	_, err = holder.Hold(t.Context(), agent.ID)
	require.ErrorIs(t, err, ErrAlreadyHeld)

	after := agentByID(t, st, agent.ID)
	assert.Equal(t, first.HeldAt.UTC(), after.HeldAt.UTC(), "the first hold stands")
}

// TestReleasingAnAgentNobodyHoldsIsRefused covers the key pressed on a row that is not held.
func TestReleasingAnAgentNobodyHoldsIsRefused(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	holder := holderOver(st, &fakeChannel{}, &settlingObserver{readings: []harness.Activity{harness.ActivityIdle}})

	_, err := holder.Release(t.Context(), ReleaseParams{AgentID: agent.ID})
	require.ErrorIs(t, err, ErrNotHeld)
}

// TestResumingAHeldAgentIsRefused pins the other way an agent could be put back to work under
// somebody holding its worktree, which is a relaunch rather than a nudge.
func TestResumingAHeldAgentIsRefused(t *testing.T) {
	st := newStore(t)
	tmux := newFakeTmux()
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	holder := holderOver(st, &fakeChannel{}, &settlingObserver{readings: []harness.Activity{harness.ActivityIdle}})

	_, err := holder.Hold(t.Context(), agent.ID)
	require.NoError(t, err)

	manager := st.agentService(agentServiceOptions{tmux: tmux, harnesses: fakeHarnesses{harness: &fakeHarness{}}})

	_, err = manager.resume.ResumeAgent(t.Context(), agent.ID)
	require.ErrorIs(t, err, ErrAgentHeld)
}

// TestHoldingAWaitingAgentWaitsForThePromptToGo is the case a stored state gets wrong twice over:
// a prompt is a write that lands the moment anybody answers it, and it is also the frame the
// interrupt is sent to clear, so it can never be the reading that confirms the interrupt worked.
func TestHoldingAWaitingAgentWaitsForThePromptToGo(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateWaiting)

	channel := &fakeChannel{}
	observer := &settlingObserver{readings: []harness.Activity{
		harness.ActivityWaiting,
		harness.ActivityWaiting,
		harness.ActivityIdle,
	}}

	held, err := holderOver(st, channel, observer).Hold(t.Context(), agent.ID)
	require.NoError(t, err)

	assert.Equal(t, 1, channel.interrupts, "a prompt holds a pending write, so it is interrupted")
	assert.NotNil(t, held.HeldAt)
	assert.True(t, held.HeldMidTurn)
	assert.Equal(t, 3, observer.reads, "and the hold waits for a frame that is not the prompt")
}

// TestHoldRefusesAPromptThatNeverClears covers the interrupt that is swallowed at a prompt, where
// the agent is still holding a write that a person answering the prompt would land.
func TestHoldRefusesAPromptThatNeverClears(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateWaiting)

	observer := &settlingObserver{readings: []harness.Activity{harness.ActivityWaiting}}

	_, err := holderOver(st, &fakeChannel{}, observer).Hold(t.Context(), agent.ID)
	require.ErrorIs(t, err, ErrHoldRefused)

	after := agentByID(t, st, agent.ID)
	assert.Nil(t, after.HeldAt, "the prompt is still there, so nobody has the tree")
}

// TestHoldReadsThePaneRatherThanTheRow pins both halves being decided now: a row that says idle
// while the pane is working must not hand over a tree.
func TestHoldReadsThePaneRatherThanTheRow(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	channel := &fakeChannel{}
	observer := &settlingObserver{readings: []harness.Activity{
		harness.ActivityWorking,
		harness.ActivityIdle,
	}}

	held, err := holderOver(st, channel, observer).Hold(t.Context(), agent.ID)
	require.NoError(t, err)

	assert.Equal(t, 1, channel.interrupts, "the pane said it was working, whatever the row said")
	assert.True(t, held.HeldMidTurn, "so letting go has a turn to carry on with")
}

// TestRebasingAHeldAgentIsRefused covers the third writer: a rebase types at the agent and then
// rewrites the branch under the very checkout somebody stopped it to take.
func TestRebasingAHeldAgentIsRefused(t *testing.T) {
	t.Parallel()

	at := time.Now()
	service := NewRebaseService(NewRebaseServiceParams{
		Agents: fakeRebaseAgents{agent: agentstore.Agent{
			ID:         "a1",
			Name:       "auth",
			WorkflowID: "w1",
			Cwd:        "/tree",
			State:      agentstore.StateIdle,
			HeldAt:     &at,
		}},
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Rebaser:   &fakeRebaser{},
	})

	_, err := service.Rebase(t.Context(), "a1")
	require.ErrorIs(t, err, ErrAgentHeld)
}
