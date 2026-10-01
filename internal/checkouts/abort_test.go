package checkouts

import (
	"encoding/json"
	"errors"
	"testing"

	"komrad/internal/git"
	"komrad/internal/gittest"
	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	agentstore "komrad/internal/store/agent"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAbortRefusals covers an abort that undoes nothing, and one that parked but found the index held.
func TestAbortRefusals(t *testing.T) {
	type testCase struct {
		name      string
		agent     agentstore.Agent
		aborter   gittest.Aborter
		wantErr   error
		wantCalls []string
	}

	tests := []testCase{
		{
			name:    "an agent whose tree something else is working in",
			agent:   agentstore.Agent{Cwd: "/locked"},
			aborter: gittest.Aborter{Sequence: git.SequenceRebase},
			wantErr: ErrBusy,
		},
		{
			name:    "a checkout with nothing part-way through",
			aborter: gittest.Aborter{Sequence: git.SequenceNone},
			wantErr: ErrAbortNothing,
		},
		{
			name: "a checkout another git command holds",
			aborter: gittest.Aborter{
				Sequence: git.SequenceRebase,
				AbortErr: git.ErrIndexLocked,
			},
			wantErr:   ErrBusy,
			wantCalls: []string{"park", "abort rebase"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, tt.agent)
			require.NoError(t, fixture.stores.Agents.SetSequenceByID(t.Context(), agent.ID, agentstore.SequenceRebase))
			locks := NewTreeLocks()
			release, taken := locks.TryLock("/locked")
			require.True(t, taken)
			t.Cleanup(release)

			_, err := fixture.newService(NewServiceParams{Aborter: &tt.aborter, Locks: locks}).Abort(t.Context(), agent.ID)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantCalls, tt.aborter.Calls)
			assert.Empty(t, fixture.readInbox(t, agent.ID))
			stored, err := fixture.stores.Agents.GetByID(t.Context(), agent.ID)
			require.NoError(t, err)
			assert.Equal(t, agentstore.SequenceRebase, stored.Sequence, "nothing was undone, so the row still says so")
		})
	}
}

// TestAbortUndoesTheSequence covers the sequence being parked and undone, and the agent told whose it was.
func TestAbortUndoesTheSequence(t *testing.T) {
	type testCase struct {
		name       string
		lastRebase *rebaseOutcome
		aborter    gittest.Aborter
		wantOurs   bool
		wantMail   []string
	}

	conflicted := &rebaseOutcome{Result: &RebaseResult{
		RebasePlan: RebasePlan{BaseSHA: "basesha"},
		Conflicted: true,
	}}

	tests := []testCase{
		{
			name:       "the rebase komrad left stopped on conflicts",
			lastRebase: conflicted,
			aborter: gittest.Aborter{
				Sequence: git.SequenceRebase,
				Onto:     "basesha",
				Branch:   "agent/a1",
			},
			wantOurs: true,
			wantMail: []string{"chose not to carry it forward", "Your branch agent/a1 is back", "Do not start the rebase again."},
		},
		{
			name:       "a rebase the agent started after komrad's stopped one",
			lastRebase: conflicted,
			aborter: gittest.Aborter{
				Sequence: git.SequenceRebase,
				Onto:     "othersha",
				Branch:   "agent/a1",
			},
			wantMail: []string{"The rebase you had part-way through", "without asking"},
		},
		{
			name: "a rebase komrad never started",
			aborter: gittest.Aborter{
				Sequence: git.SequenceRebase,
				Onto:     "basesha",
				Branch:   "agent/a1",
			},
			wantMail: []string{"The rebase you had part-way through"},
		},
		{
			name: "a cherry-pick the agent started",
			aborter: gittest.Aborter{
				Sequence: git.SequenceCherryPick,
				Branch:   "agent/a1",
			},
			wantMail: []string{"The cherry-pick you had part-way through"},
		},
		{
			name:     "a sequence that leaves the checkout detached",
			aborter:  gittest.Aborter{Sequence: git.SequenceMerge},
			wantMail: []string{"The merge you had", "Your checkout is back"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{})
			require.NoError(t, fixture.stores.Agents.SetSequenceByID(t.Context(), agent.ID, agentstore.Sequence(tt.aborter.Sequence)))
			if tt.lastRebase != nil {
				raw, err := json.Marshal(tt.lastRebase)
				require.NoError(t, err)
				require.NoError(t, fixture.stores.Agents.SetLastRebaseByID(t.Context(), agent.ID, raw))
			}
			sequence := tt.aborter.Sequence

			result, err := fixture.newService(NewServiceParams{Aborter: &tt.aborter}).Abort(t.Context(), agent.ID)
			require.NoError(t, err)

			assert.Equal(t, sequence, result.Sequence)
			assert.Equal(t, tt.aborter.Branch, result.Branch)
			assert.Equal(t, tt.wantOurs, result.Ours)
			assert.Equal(t, []string{"park", "abort " + string(sequence)}, tt.aborter.Calls)
			assert.Equal(t, []string{result.ParkedRef}, tt.aborter.Parked)
			assert.Contains(t, result.ParkedRef, "refs/komrad/prerefresh/"+agent.ID+"/")
			assert.Equal(t, 1, fixture.checkoutSync.Wakes)

			stored, err := fixture.stores.Agents.GetByID(t.Context(), agent.ID)
			require.NoError(t, err)
			assert.Equal(t, agentstore.SequenceNone, stored.Sequence)

			inbox := fixture.readInbox(t, agent.ID)
			require.Len(t, inbox, 1)
			for _, want := range append(tt.wantMail, result.ParkedRef) {
				assert.Contains(t, inbox[0], want)
			}
			assert.NotContains(t, inbox[0], "interrupted")
		})
	}
}

// TestAbortTellsAnAgentWhoseTurnWasCut covers the agent being stopped first and told so whether or not the abort went through.
func TestAbortTellsAnAgentWhoseTurnWasCut(t *testing.T) {
	type testCase struct {
		name     string
		abortErr error
		wantMail string
	}

	tests := []testCase{
		{
			name:     "an abort that failed after the turn was cut",
			abortErr: errors.New("could not detach HEAD"),
			wantMail: "but the abort failed: could not detach HEAD",
		},
		{
			name:     "an abort that went through",
			wantMail: "Your turn was interrupted so that the rebase in your worktree could be aborted.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{Pane: "%1", State: agentstore.StateThinking})
			channel := &harnesstest.Channel{}
			observer := harnesstest.NewActivityObserver(harness.ActivityWorking, harness.ActivityIdle)
			aborter := &gittest.Aborter{
				Sequence: git.SequenceRebase,
				Branch:   "agent/a1",
				AbortErr: tt.abortErr,
			}

			_, err := fixture.newService(NewServiceParams{
				Settler: buildSettler(channel, observer),
				Aborter: aborter,
			}).Abort(t.Context(), agent.ID)
			if tt.abortErr != nil {
				require.ErrorIs(t, err, tt.abortErr)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, 1, channel.Interrupts)
			inbox := fixture.readInbox(t, agent.ID)
			require.Len(t, inbox, 1)
			assert.Contains(t, inbox[0], tt.wantMail)
		})
	}
}
