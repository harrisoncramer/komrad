package checkouts

import (
	"encoding/json"
	"testing"

	"komrad/internal/git"
	"komrad/internal/gittest"
	agentstore "komrad/internal/store/agent"
	taskstore "komrad/internal/store/task"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAbortRefusals covers an abort that undoes nothing, one under a working writer, and one that
// parked but found the index held.
func TestAbortRefusals(t *testing.T) {
	type testCase struct {
		name      string
		agent     agentstore.Agent
		aborter   gittest.Mover
		wantErr   error
		wantCalls []string
	}

	tests := []testCase{
		{
			name:    "an agent whose tree something else is working in",
			agent:   agentstore.Agent{Cwd: "/locked"},
			aborter: gittest.Mover{Sequence: git.SequenceRebase},
			wantErr: errBusy,
		},
		{
			name:    "a writer part way through a turn",
			agent:   agentstore.Agent{Pane: "%1", State: agentstore.StateThinking},
			aborter: gittest.Mover{Sequence: git.SequenceRebase},
			wantErr: errWriterNotAtRest,
		},
		{
			name:    "a checkout with nothing part-way through",
			aborter: gittest.Mover{Sequence: git.SequenceNone},
			wantErr: ErrAbortNothing,
		},
		{
			name: "a checkout another git command holds",
			aborter: gittest.Mover{
				Sequence: git.SequenceRebase,
				AbortErr: git.ErrIndexLocked,
			},
			wantErr:   errBusy,
			wantCalls: []string{"park", "abort rebase"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, tt.agent)
			require.NoError(t, fixture.stores.Tasks.SetSequence(t.Context(), *agent.TaskID, taskstore.SequenceRebase))
			locks := NewTreeLocks()
			release, taken := locks.TryLock("/locked")
			require.True(t, taken)
			t.Cleanup(release)

			_, err := fixture.newService(NewServiceParams{Git: &tt.aborter, Locks: locks}).Abort(t.Context(), taskByID(*agent.TaskID))

			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, tt.wantCalls, tt.aborter.Calls)
			assert.Empty(t, fixture.readInbox(t, agent.ID))
			assert.Equal(t, taskstore.SequenceRebase, fixture.readCheckout(t, agent).Sequence, "nothing was undone, so the row still says so")
		})
	}
}

// TestAbortUndoesTheSequence covers komrad parking and undoing the sequence, and telling the agent whose it was.
func TestAbortUndoesTheSequence(t *testing.T) {
	type testCase struct {
		name       string
		lastRebase *rebaseOutcome
		aborter    gittest.Mover
		wantOurs   bool
		wantMail   []string
	}

	conflicted := &rebaseOutcome{Result: RebaseResult{
		RebasePlan: RebasePlan{BaseSHA: "basesha"},
		Conflicted: true,
	}}

	tests := []testCase{
		{
			name:       "the rebase komrad left stopped on conflicts",
			lastRebase: conflicted,
			aborter: gittest.Mover{
				Sequence: git.SequenceRebase,
				Onto:     "basesha",
				State:    git.TreeState{Branch: "agent/a1"},
			},
			wantOurs: true,
			wantMail: []string{"chose not to carry it forward", "Your branch agent/a1 is back", "Do not start the rebase again."},
		},
		{
			name:       "a rebase the agent started after komrad's stopped one",
			lastRebase: conflicted,
			aborter: gittest.Mover{
				Sequence: git.SequenceRebase,
				Onto:     "othersha",
				State:    git.TreeState{Branch: "agent/a1"},
			},
			wantMail: []string{"The rebase you had part-way through", "without asking"},
		},
		{
			name: "a rebase komrad never started",
			aborter: gittest.Mover{
				Sequence: git.SequenceRebase,
				Onto:     "basesha",
				State:    git.TreeState{Branch: "agent/a1"},
			},
			wantMail: []string{"The rebase you had part-way through"},
		},
		{
			name: "a cherry-pick the agent started",
			aborter: gittest.Mover{
				Sequence: git.SequenceCherryPick,
				State:    git.TreeState{Branch: "agent/a1"},
			},
			wantMail: []string{"The cherry-pick you had part-way through"},
		},
		{
			name:     "a sequence that leaves the checkout detached",
			aborter:  gittest.Mover{Sequence: git.SequenceMerge},
			wantMail: []string{"The merge you had", "Your checkout is back"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{})
			require.NoError(t, fixture.stores.Tasks.SetSequence(t.Context(), *agent.TaskID, taskstore.Sequence(tt.aborter.Sequence)))
			if tt.lastRebase != nil {
				raw, err := json.Marshal(tt.lastRebase)
				require.NoError(t, err)
				require.NoError(t, fixture.stores.Tasks.SetLastRebase(t.Context(), *agent.TaskID, raw))
			}
			sequence := tt.aborter.Sequence

			result, err := fixture.newService(NewServiceParams{Git: &tt.aborter}).Abort(t.Context(), taskByID(*agent.TaskID))
			require.NoError(t, err)

			assert.Equal(t, sequenceOf(sequence), result.Sequence)
			assert.Equal(t, tt.aborter.State.Branch, result.Branch)
			assert.Equal(t, tt.wantOurs, result.Ours)
			assert.Equal(t, []string{"park", "abort " + string(sequence)}, tt.aborter.Calls)
			assert.Equal(t, []string{result.ParkedRef}, tt.aborter.Parked)
			assert.Contains(t, result.ParkedRef, "refs/komrad/aborted/"+*agent.TaskID+"/")
			assert.Equal(t, 1, fixture.checkoutSync.Wakes)

			assert.Equal(t, taskstore.SequenceNone, fixture.readCheckout(t, agent).Sequence)

			inbox := fixture.readInbox(t, agent.ID)
			require.Len(t, inbox, 1)
			for _, want := range append(tt.wantMail, result.ParkedRef) {
				assert.Contains(t, inbox[0], want)
			}
		})
	}
}
