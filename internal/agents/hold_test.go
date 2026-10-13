package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/config"
	"komrad/internal/agenterr"
	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	"komrad/internal/mail"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	"komrad/internal/tmuxtest"
	"komrad/lib/clock/clocktest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newHolder builds a holder over the test store with a clock the test moves itself, so waiting
// for an agent to settle costs no real time.
func newHolder(st *testStore, channel *harnesstest.Channel, observer harness.Observer) *Holder {
	return NewHolder(NewHolderParams{
		Agents: st.Agents,
		MailSender: mail.NewMailService(mail.NewMailServiceParams{
			Mail:      st.Mail,
			Agents:    st.Agents,
			Workflows: st.Workflows,
			Config:    config.Mail{},
		}),
		Settler: settlerOver(channel, observer),
	})
}

// settlerOver builds a settler whose clock walks the budget by itself.
func settlerOver(channel harness.Channel, observer harness.Observer) *Settler {
	at := time.Now()

	return NewSettler(NewSettlerParams{
		Channel:  channel,
		Observer: observer,
		Clock: func() time.Time {
			at = at.Add(settleInterval)

			return at
		},
		Sleeper: &clocktest.Sleeper{},
	})
}

// TestHoldWaitsForTheAgentToStop is the whole safety argument: the interrupt is keys typed into a
// terminal, so what may claim the worktree is an agent observed to have come to rest.
func TestHoldWaitsForTheAgentToStop(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateThinking)

	channel := &harnesstest.Channel{}
	observer := harnesstest.NewActivityObserver(
		harness.ActivityWorking,
		harness.ActivityWorking,
		harness.ActivityIdle,
	)

	held, err := newHolder(st, channel, observer).Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.NoError(t, err)

	assert.NotNil(t, held.Hold.At, "the hold is written down only once the agent has stopped")
	assert.True(t, held.Hold.MidTurn, "and it says the turn it cut was still running")
	assert.Equal(t, 1, channel.Interrupts)
	assert.Equal(t, 3, observer.Reads, "the pane was read until it settled")
}

// TestHoldRecordsTheAgentThatTookIt covers the holder written with the hold and cleared with it.
func TestHoldRecordsTheAgentThatTookIt(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)
	orchestrator := seed(t, st, "conductor", "%2", agentstore.StateIdle)

	holder := newHolder(st, &harnesstest.Channel{}, harnesstest.NewActivityObserver(harness.ActivityIdle))

	held, err := holder.Hold(t.Context(), HoldParams{AgentID: agent.ID, ByAgentID: orchestrator.ID})
	require.NoError(t, err)
	assert.Equal(t, &orchestrator.ID, held.Hold.ByAgentID)

	released, err := holder.Release(t.Context(), ReleaseParams{AgentID: agent.ID})
	require.NoError(t, err)
	assert.Nil(t, released.Hold.ByAgentID)
}

// TestHoldRefusesAnAgentHoldingItself covers the hold nobody would know to let go.
func TestHoldRefusesAnAgentHoldingItself(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)
	channel := &harnesstest.Channel{}

	_, err := newHolder(st, channel, harnesstest.NewActivityObserver(harness.ActivityIdle)).Hold(t.Context(), HoldParams{
		AgentID:   agent.ID,
		ByAgentID: agent.ID,
	})
	require.ErrorIs(t, err, errHoldSelf)
	assert.Zero(t, channel.Interrupts, "the agent was never interrupted")

	after := agentByID(t, st, agent.ID)
	assert.Nil(t, after.Hold.At, "a refused hold claims nothing")
}

// TestHoldRefusesAnAgentThatWillNotStop covers the interrupt that never lands, where claiming the
// worktree would put a person's editor on top of a writer.
func TestHoldRefusesAnAgentThatWillNotStop(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateThinking)

	observer := harnesstest.NewActivityObserver(harness.ActivityWorking)

	_, err := newHolder(st, &harnesstest.Channel{}, observer).Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.ErrorIs(t, err, agenterr.ErrUnsettled)
	require.ErrorContains(t, err, "had not stopped")

	after := agentByID(t, st, agent.ID)
	assert.Nil(t, after.Hold.At, "a refused hold claims nothing")
}

// TestHoldRefusesAPaneItCannotRead pins the state that means the reading failed rather than that
// the agent is idle, which is the one case nothing can call safe.
func TestHoldRefusesAPaneItCannotRead(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateThinking)

	observer := &harnesstest.Observer{Err: errors.New("the frame could not be captured")}

	_, err := newHolder(st, &harnesstest.Channel{}, observer).Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.ErrorIs(t, err, agenterr.ErrUnsettled)
	require.ErrorContains(t, err, "could not be read")

	after := agentByID(t, st, agent.ID)
	assert.Nil(t, after.Hold.At)
}

// TestHoldRefusesAFrameItCannotMatchWithoutTypingAtIt is the pair of the failed capture: a frame
// that matched none of the harness's patterns says neither that a turn is running nor that the
// agent has stopped, and the keys sent on that guess open a dialog nobody is there to dismiss.
func TestHoldRefusesAFrameItCannotMatchWithoutTypingAtIt(t *testing.T) {
	for _, activity := range []harness.Activity{harness.ActivityUnrecognized, harness.ActivityUnknown} {
		t.Run(activity.String(), func(t *testing.T) {
			st := newStore(t)
			agent := seed(t, st, "telescope", "%1", agentstore.StateThinking)

			channel := &harnesstest.Channel{}
			observer := harnesstest.NewActivityObserver(activity)

			_, err := newHolder(st, channel, observer).Hold(t.Context(), HoldParams{AgentID: agent.ID})
			require.ErrorIs(t, err, agenterr.ErrUnsettled)
			require.ErrorContains(t, err, "could not be read")

			assert.Zero(t, channel.Interrupts, "nothing said there was a turn to cut")
			assert.Equal(t, 1, observer.Reads, "and the refusal costs no settle")

			after := agentByID(t, st, agent.ID)
			assert.Nil(t, after.Hold.At, "a refused hold claims nothing")
		})
	}
}

// TestHoldingAnIdleAgentTypesNothingAtIt covers holding an agent that was already at rest, which
// is reading rather than interrupting.
func TestHoldingAnIdleAgentTypesNothingAtIt(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	channel := &harnesstest.Channel{}
	observer := harnesstest.NewActivityObserver(harness.ActivityIdle)

	held, err := newHolder(st, channel, observer).Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.NoError(t, err)

	assert.Zero(t, channel.Interrupts, "there was no turn to cut")
	assert.False(t, held.Hold.MidTurn)
}

// TestReleasingATurnThatWasCutSaysWhatChanged is what makes an agent's next turn start from the
// truth rather than from what it remembers.
func TestReleasingATurnThatWasCutSaysWhatChanged(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateThinking)

	channel := &harnesstest.Channel{}
	holder := newHolder(st, channel, harnesstest.NewActivityObserver(
		harness.ActivityWorking,
		harness.ActivityIdle,
	))

	_, err := holder.Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.NoError(t, err)

	released, err := holder.Release(t.Context(), ReleaseParams{
		AgentID: agent.ID,
		Said:    "I changed review.go while you were held",
	})
	require.NoError(t, err)

	assert.Nil(t, released.Hold.At, "nobody is holding it now")
	assert.Empty(t, channel.Sent, "nothing is typed into its pane")

	waiting, err := st.Mail.Claim(t.Context(), agent.ID, 10)
	require.NoError(t, err)
	require.Len(t, waiting, 1)
	assert.Equal(t, "I changed review.go while you were held", waiting[0].Body)
	assert.Nil(t, waiting[0].FromAgentID, "the panel let it go")
}

// TestReleasingAnAgentThatHadFinishedMailsNothing covers the agent whose own turn ended before the
// hold, which mail would put back to work nobody asked for.
func TestReleasingAnAgentThatHadFinishedMailsNothing(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	channel := &harnesstest.Channel{}
	holder := newHolder(st, channel, harnesstest.NewActivityObserver(harness.ActivityIdle))

	_, err := holder.Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.NoError(t, err)

	released, err := holder.Release(t.Context(), ReleaseParams{
		AgentID: agent.ID,
		Said:    "I changed review.go while you were held",
	})
	require.NoError(t, err)

	assert.Nil(t, released.Hold.At)

	pending, err := st.Mail.CountPending(t.Context(), agent.ID)
	require.NoError(t, err)
	assert.Zero(t, pending, "there was no cut turn to carry on with")
}

// TestHoldingAnAgentTwiceIsRefused pins the second person, or the second keypress, finding it
// already taken rather than quietly moving the stamp.
func TestHoldingAnAgentTwiceIsRefused(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	holder := newHolder(st, &harnesstest.Channel{}, harnesstest.NewActivityObserver(harness.ActivityIdle))

	first, err := holder.Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.NoError(t, err)

	_, err = holder.Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.ErrorIs(t, err, errAlreadyHeld)

	after := agentByID(t, st, agent.ID)
	assert.Equal(t, first.Hold.At.UTC(), after.Hold.At.UTC(), "the first hold stands")
}

// TestReleasingAnAgentNobodyHoldsIsRefused covers the key pressed on a row that is not held.
func TestReleasingAnAgentNobodyHoldsIsRefused(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	holder := newHolder(st, &harnesstest.Channel{}, harnesstest.NewActivityObserver(harness.ActivityIdle))

	_, err := holder.Release(t.Context(), ReleaseParams{AgentID: agent.ID})
	require.ErrorIs(t, err, ErrNotHeld)
}

// TestResumingAHeldAgentIsRefused pins the other way an agent could be put back to work under
// somebody holding its worktree, which is a relaunch rather than a nudge.
func TestResumingAHeldAgentIsRefused(t *testing.T) {
	st := newStore(t)
	tmux := tmuxtest.NewFake()
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	holder := newHolder(st, &harnesstest.Channel{}, harnesstest.NewActivityObserver(harness.ActivityIdle))

	_, err := holder.Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.NoError(t, err)

	manager := st.agentService(agentServiceOptions{tmux: tmux, harnesses: harnesstest.Registry{Harness: &harnesstest.Fake{}}})

	_, err = manager.resume.ResumeAgent(t.Context(), ResumeAgentParams{ID: agent.ID})
	require.ErrorIs(t, err, agenterr.ErrAgentHeld)
}

// TestHoldingAWaitingAgentWaitsForThePromptToGo is the case a stored state gets wrong twice over:
// a prompt is a write that lands the moment anybody answers it, and it is also the frame the
// interrupt is sent to clear, so it can never be the reading that confirms the interrupt worked.
func TestHoldingAWaitingAgentWaitsForThePromptToGo(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateWaiting)

	channel := &harnesstest.Channel{}
	observer := harnesstest.NewActivityObserver(
		harness.ActivityWaiting,
		harness.ActivityWaiting,
		harness.ActivityIdle,
	)

	held, err := newHolder(st, channel, observer).Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.NoError(t, err)

	assert.Equal(t, 1, channel.Interrupts, "a prompt holds a pending write, so it is interrupted")
	assert.NotNil(t, held.Hold.At)
	assert.True(t, held.Hold.MidTurn)
	assert.Equal(t, 3, observer.Reads, "and the hold waits for a frame that is not the prompt")
}

// TestHoldRefusesAPromptThatNeverClears covers an interrupt the prompt swallows, where
// the agent is still holding a write that a person answering the prompt would land.
func TestHoldRefusesAPromptThatNeverClears(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateWaiting)

	observer := harnesstest.NewActivityObserver(harness.ActivityWaiting)

	_, err := newHolder(st, &harnesstest.Channel{}, observer).Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.ErrorIs(t, err, agenterr.ErrUnsettled)

	after := agentByID(t, st, agent.ID)
	assert.Nil(t, after.Hold.At, "the prompt is still there, so nobody has the tree")
}

// TestHoldReadsThePaneRatherThanTheRow pins that komrad decides both halves now: a row that says
// idle while the pane is working must not hand over a tree.
func TestHoldReadsThePaneRatherThanTheRow(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateIdle)

	channel := &harnesstest.Channel{}
	observer := harnesstest.NewActivityObserver(
		harness.ActivityWorking,
		harness.ActivityIdle,
	)

	held, err := newHolder(st, channel, observer).Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.NoError(t, err)

	assert.Equal(t, 1, channel.Interrupts, "the pane said it was working, whatever the row said")
	assert.True(t, held.Hold.MidTurn, "so letting go has a turn to carry on with")
}

// TestReleaseKeepsTheHoldWhenTheMailIsRefused covers a release whose word could not reach the agent.
func TestReleaseKeepsTheHoldWhenTheMailIsRefused(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateThinking)

	holder := NewHolder(NewHolderParams{
		Agents:     st.Agents,
		MailSender: mailRefuser{},
		Settler: settlerOver(&harnesstest.Channel{}, harnesstest.NewActivityObserver(
			harness.ActivityWorking,
			harness.ActivityIdle,
		)),
	})

	_, err := holder.Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.NoError(t, err)

	_, err = holder.Release(t.Context(), ReleaseParams{AgentID: agent.ID, Said: "carry on"})
	require.ErrorIs(t, err, mail.ErrMailboxFull)

	stored, err := st.Agents.GetByID(t.Context(), agent.ID)
	require.NoError(t, err)
	assert.True(t, stored.Hold.IsHeld(), "an agent let go without the word would carry on as if nothing changed")
}

type mailRefuser struct{}

func (mailRefuser) SendMail(context.Context, mail.SendMailParams) (*mailstore.Mail, error) {
	return nil, mail.ErrMailboxFull
}

// TestReleasingInSilenceLetsGoOfAnInterruptedAgent covers a release asking for nothing to be said.
func TestReleasingInSilenceLetsGoOfAnInterruptedAgent(t *testing.T) {
	st := newStore(t)
	agent := seed(t, st, "telescope", "%1", agentstore.StateThinking)

	channel := &harnesstest.Channel{}
	holder := newHolder(st, channel, harnesstest.NewActivityObserver(
		harness.ActivityWorking,
		harness.ActivityIdle,
	))

	held, err := holder.Hold(t.Context(), HoldParams{AgentID: agent.ID})
	require.NoError(t, err)
	require.True(t, held.Hold.MidTurn, "so the speaking path is the one being skipped")

	released, err := holder.Release(t.Context(), ReleaseParams{AgentID: agent.ID})
	require.NoError(t, err)

	assert.Nil(t, released.Hold.At, "nobody is holding it now")
	assert.Empty(t, channel.Sent)
}
