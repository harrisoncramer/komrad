package checkouts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/config"
	"weave/internal/agents"
	"weave/internal/caller"
	"weave/internal/git"
	"weave/internal/gittest"
	"weave/internal/harness"
	"weave/internal/harnesstest"
	"weave/internal/mail"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"
	"weave/lib/xptr"
	"weave/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rebaseFixture struct {
	stores     *storetest.Stores
	workflowID string
}

// treeReadRecorder notes how many pane readings had been taken by the time the tree was read.
type treeReadRecorder struct {
	*gittest.Rebaser
	observer        *settlingObserver
	readsBeforeTree int
}

func (r *treeReadRecorder) ReadTreeState(ctx context.Context, dir string) (git.TreeState, error) {
	r.readsBeforeTree = r.observer.reads

	return r.Rebaser.ReadTreeState(ctx, dir)
}

func newRebaseFixture(t *testing.T) *rebaseFixture {
	t.Helper()

	stores := storetest.Open(t)

	return &rebaseFixture{
		stores: stores,
		workflowID: stores.SeedWorkflow(t, workflowstore.Config{
			Name:     "flow",
			Worktree: workflowstore.WorktreeConfig{Enabled: true},
		}).ID,
	}
}

// seed records an idle agent named auth in /tree unless the agent says otherwise.
func (f *rebaseFixture) seed(t *testing.T, agent agentstore.Agent) *agentstore.Agent {
	t.Helper()

	if agent.Name == "" {
		agent.Name = "auth"
	}
	if agent.Cwd == "" {
		agent.Cwd = "/tree"
	}
	if agent.WorkflowID == "" {
		agent.WorkflowID = f.workflowID
	}

	return f.stores.SeedAgent(t, agent)
}

// newService builds a rebase service over the real stores.
func (f *rebaseFixture) newService(params NewRebaseServiceParams) *RebaseService {
	params.Agents = f.stores.Agents
	params.Workflows = f.stores.Workflows
	if params.Rebaser == nil {
		params.Rebaser = &gittest.Rebaser{State: git.TreeState{Branch: "agent/a1"}}
	}

	return NewRebaseService(params)
}

func (f *rebaseFixture) newMailer() *mail.MailService {
	return mail.NewMailService(mail.NewMailServiceParams{
		MailReadWriter: f.stores.Mail,
		Agents:         f.stores.Agents,
		Workflows:      f.stores.Workflows,
		Config:         config.Mail{},
	})
}

func (f *rebaseFixture) readInbox(t *testing.T, id string) []string {
	t.Helper()

	sent, err := f.stores.Mail.ListPage(t.Context(), id, 10, nil)
	require.NoError(t, err)

	return xslice.Map(sent, func(piece mailstore.Mail) string {
		return piece.Body
	})
}

// TestRebaseRefusals covers every state of the row a rebase declines.
func TestRebaseRefusals(t *testing.T) {
	tests := []struct {
		name    string
		lay     func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent
		wantErr error
	}{
		{
			name: "an agent part-way through a turn, since the files would move under it",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				return fixture.seed(t, agentstore.Agent{State: agentstore.StateThinking})
			},
			wantErr: ErrRebaseBusy,
		},
		{
			name: "an agent waiting on a question, which is also an unfinished turn",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				return fixture.seed(t, agentstore.Agent{State: agentstore.StateWaiting})
			},
			wantErr: ErrRebaseBusy,
		},
		{
			name: "an agent whose pane could not be read, which says nothing about whether it is working",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				return fixture.seed(t, agentstore.Agent{State: agentstore.StateUncertain, Pane: "%1"})
			},
			wantErr: ErrRebaseBusy,
		},
		{
			name: "an agent whose worktree is still being provisioned",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				return fixture.seed(t, agentstore.Agent{State: agentstore.StateSettingUp})
			},
			wantErr: ErrRebaseBusy,
		},
		{
			name: "a branch a human made, whose history is not weave's to rewrite",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				return fixture.seed(t, agentstore.Agent{AdoptedBranch: "harry/spike"})
			},
			wantErr: ErrRebaseAdopted,
		},
		{
			name: "a child, since rebasing one rewrites its parent's branch",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				parent := fixture.seed(t, agentstore.Agent{Name: "parent"})

				return fixture.seed(t, agentstore.Agent{ParentAgentID: &parent.ID})
			},
			wantErr: ErrRebaseNoWorktree,
		},
		{
			name: "an agent told to keep the directory it was given",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				return fixture.seed(t, agentstore.Agent{ProvisionEnabled: xptr.Ptr(false)})
			},
			wantErr: ErrRebaseNoWorktree,
		},
		{
			name: "an agent whose directory is gone",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				agent := fixture.seed(t, agentstore.Agent{})
				require.NoError(t, fixture.stores.Agents.SetCheckout(t.Context(), agent.ID, agentstore.SetCheckoutParams{WorktreeRemoved: true}))

				return agent
			},
			wantErr: ErrRebaseNoWorktree,
		},
		{
			name: "an agent in a workflow that makes no worktrees",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				flow := fixture.stores.SeedWorkflow(t, workflowstore.Config{Name: "no-worktrees"})

				return fixture.seed(t, agentstore.Agent{WorkflowID: flow.ID})
			},
			wantErr: ErrRebaseNoWorktree,
		},
		{
			name: "an archived agent",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				agent := fixture.seed(t, agentstore.Agent{})
				_, err := fixture.stores.Agents.Archive(t.Context(), []string{agent.ID})
				require.NoError(t, err)

				return agent
			},
			wantErr: agents.ErrAgentArchived,
		},
		{
			name: "a held agent, before anything types at it or moves the branch",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				agent := fixture.seed(t, agentstore.Agent{})
				_, err := fixture.stores.Agents.Hold(t.Context(), agentstore.HoldParams{ID: agent.ID, At: time.Now()})
				require.NoError(t, err)

				return agent
			},
			wantErr: agents.ErrAgentHeld,
		},
		{
			name: "an agent whose tree something else is working in",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				return fixture.seed(t, agentstore.Agent{Cwd: "/locked"})
			},
			wantErr: ErrRebaseBusy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := tt.lay(t, fixture)
			locks := NewTreeLocks()
			release, taken := locks.TryLock("/locked")
			require.True(t, taken)
			t.Cleanup(release)
			rebaser := &gittest.Rebaser{State: git.TreeState{Branch: "agent/a1"}}

			_, err := fixture.newService(NewRebaseServiceParams{Rebaser: rebaser, Locks: locks}).Rebase(t.Context(), agent.ID)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, rebaser.Calls)
		})
	}
}

// TestTreeLocksHoldOnePathAtATime covers two agents in different trees not blocking each other.
func TestTreeLocksHoldOnePathAtATime(t *testing.T) {
	locks := NewTreeLocks()

	release, taken := locks.TryLock("/one")
	require.True(t, taken)

	_, again := locks.TryLock("/one")
	assert.False(t, again, "the same path is held once")

	other, taken := locks.TryLock("/two")
	assert.True(t, taken, "a different path is not blocked")
	other()

	release()

	_, afterRelease := locks.TryLock("/one")
	assert.True(t, afterRelease, "releasing hands the path back")
}

// TestTreeLocksRefuseAnEmptyPath covers the fail-open a blank path would otherwise be.
func TestTreeLocksRefuseAnEmptyPath(t *testing.T) {
	release, taken := NewTreeLocks().TryLock("")

	assert.False(t, taken, "an empty path is refused")
	assert.Nil(t, release)
}

// TestRebaseRefusesATreeGitWillNotMove covers the checkout's own refusals costing the agent no turn.
func TestRebaseRefusesATreeGitWillNotMove(t *testing.T) {
	tests := []struct {
		name    string
		state   git.TreeState
		wantErr error
	}{
		{
			name: "a path the index is holding, which nothing could put back if the move went wrong",
			state: git.TreeState{
				Branch:      "agent/a1",
				HiddenPaths: []string{"own.txt"},
			},
			wantErr: ErrRebaseHiddenPaths,
		},
		{
			name: "a tree holding tracked changes",
			state: git.TreeState{
				Branch:         "agent/a1",
				TrackedChanges: true,
			},
			wantErr: ErrRebaseDirty,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{Pane: "%1"})
			channel := &harnesstest.Channel{}
			observer := &settlingObserver{readings: []harness.Activity{harness.ActivityWorking}}

			_, err := fixture.newService(NewRebaseServiceParams{
				Settler: buildSettler(channel, observer),
				Rebaser: &gittest.Rebaser{State: tt.state},
			}).Rebase(t.Context(), agent.ID)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Zero(t, channel.Interrupts, "a tree that cannot move costs the agent no turn")
			assert.Zero(t, observer.reads)
		})
	}
}

// TestRebaseMovesTheBranch covers the git calls a rebase makes and what reaches the row.
func TestRebaseMovesTheBranch(t *testing.T) {
	tests := []struct {
		name        string
		rebaser     *gittest.Rebaser
		wantCalls   []string
		wantCutoff  string
		wantStopped bool
	}{
		{
			name: "a rebase stopped on conflicts",
			rebaser: &gittest.Rebaser{
				State:     git.TreeState{Branch: "agent/a1"},
				ReplayErr: git.ErrRebaseConflicted,
			},
			wantCalls:   []string{"park", "rebase"},
			wantStopped: true,
		},
		{
			name: "a squash-merged branch replays from the cutoff, since a plain rebase would collide with the landed content",
			rebaser: &gittest.Rebaser{
				State: git.TreeState{Branch: "agent/a1"},
				Prediction: git.Prediction{
					Cutoff: "cutoffsha",
					Landed: 2,
					Replay: 1,
				},
			},
			wantCalls:  []string{"park", "rebaseOnto"},
			wantCutoff: "cutoffsha",
		},
		{
			name: "a branch with work of its own replays whole",
			rebaser: &gittest.Rebaser{
				State:      git.TreeState{Branch: "agent/a1"},
				Prediction: git.Prediction{Replay: 1},
			},
			wantCalls: []string{"park", "rebase"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{})

			result, err := fixture.newService(NewRebaseServiceParams{Rebaser: tt.rebaser}).Rebase(t.Context(), agent.ID)
			require.NoError(t, err)

			assert.Equal(t, tt.wantCalls, tt.rebaser.Calls)
			assert.Len(t, tt.rebaser.Parked, 1)
			assert.Equal(t, tt.wantStopped, result.Conflicted)
			assert.Equal(t, tt.wantCutoff, result.Cutoff)
			assert.Equal(t, tt.wantCutoff, tt.rebaser.Cutoff)
			assert.Equal(t, tt.rebaser.Prediction.Replay, result.Replay)
			assert.False(t, result.Moved, "work was replayed or stopped, so the branch was not merely moved")

			stored, err := fixture.stores.Agents.GetByID(t.Context(), agent.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.wantStopped, stored.SequenceInProgress)
		})
	}
}

// TestRebaseSettlesTheAgentBeforeItMoves is the safety argument for the row being a pane reading
// seconds old: the pane is read as it is now, a turn in flight is cut, and nothing is parked or
// moved until the agent is seen to have come to rest.
func TestRebaseSettlesTheAgentBeforeItMoves(t *testing.T) {
	tests := []struct {
		name           string
		pane           string
		readings       []harness.Activity
		observeErr     error
		wantErr        error
		wantSaid       string
		wantInterrupts int
		wantReads      int
		wantCalls      []string
	}{
		{
			name:           "an agent that will not stop",
			pane:           "%1",
			readings:       []harness.Activity{harness.ActivityWorking},
			wantErr:        agents.ErrUnsettled,
			wantSaid:       "had not stopped",
			wantInterrupts: 1,
		},
		{
			name:       "a pane that could not be read",
			pane:       "%1",
			observeErr: errors.New("the pane is gone"),
			wantErr:    agents.ErrUnsettled,
			wantSaid:   "could not be read",
		},
		{
			name:     "a frame that matched nothing, which is no evidence of rest",
			pane:     "%1",
			readings: []harness.Activity{harness.ActivityUnrecognized},
			wantErr:  agents.ErrUnsettled,
			wantSaid: "could not be read",
		},
		{
			name:      "an agent at rest, which is read rather than interrupted",
			pane:      "%1",
			readings:  []harness.Activity{harness.ActivityIdle},
			wantReads: 1,
			wantCalls: []string{"park", "rebase"},
		},
		{
			name:           "an agent part-way through a turn its row does not show yet",
			pane:           "%1",
			readings:       []harness.Activity{harness.ActivityWorking, harness.ActivityWorking, harness.ActivityIdle},
			wantInterrupts: 1,
			wantReads:      3,
			wantCalls:      []string{"park", "rebase"},
		},
		{
			name:      "an agent with no pane, which has nothing running",
			readings:  []harness.Activity{harness.ActivityWorking},
			wantCalls: []string{"park", "rebase"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{Pane: tt.pane})
			channel := &harnesstest.Channel{}
			observer := &settlingObserver{readings: tt.readings, err: tt.observeErr}
			rebaser := &treeReadRecorder{
				Rebaser:  &gittest.Rebaser{State: git.TreeState{Branch: "agent/a1"}},
				observer: observer,
			}

			_, err := fixture.newService(NewRebaseServiceParams{
				Settler: buildSettler(channel, observer),
				Rebaser: rebaser,
			}).Rebase(t.Context(), agent.ID)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Contains(t, err.Error(), tt.wantSaid)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantReads, observer.reads)
				assert.Equal(t, tt.wantReads, rebaser.readsBeforeTree, "the plan reads the tree only once the agent has stopped")
			}

			assert.Equal(t, tt.wantInterrupts, channel.Interrupts)
			assert.Equal(t, tt.wantCalls, rebaser.Calls, "nothing is parked or moved under an agent still working")
		})
	}
}

// TestRebaseRereadsTheRowOnceTheAgentSettles covers the row changing while the agent was being settled.
func TestRebaseRereadsTheRowOnceTheAgentSettles(t *testing.T) {
	tests := []struct {
		name      string
		settling  func(t *testing.T, fixture *rebaseFixture, id string)
		readings  []harness.Activity
		wantErr   error
		wantCalls []string
		wantMail  string
	}{
		{
			name: "a row stamped thinking after the interrupt",
			settling: func(t *testing.T, fixture *rebaseFixture, id string) {
				require.NoError(t, fixture.stores.Agents.SetStateByID(t.Context(), id, agentstore.StateThinking))
			},
			readings:  []harness.Activity{harness.ActivityWorking, harness.ActivityIdle},
			wantCalls: []string{"park", "rebase"},
			wantMail:  "Your branch is still the same branch",
		},
		{
			name:     "a hold landing on an agent whose turn was cut",
			settling: holdWhileSettling,
			readings: []harness.Activity{harness.ActivityWorking, harness.ActivityIdle},
			wantErr:  agents.ErrAgentHeld,
			wantMail: "interrupted",
		},
		{
			name:     "a hold landing on an agent that was already at rest",
			settling: holdWhileSettling,
			readings: []harness.Activity{harness.ActivityIdle},
			wantErr:  agents.ErrAgentHeld,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{Pane: "%1"})
			channel := &harnesstest.Channel{}
			observer := &settlingObserver{
				readings: tt.readings,
				onFirst:  func() { tt.settling(t, fixture, agent.ID) },
			}
			rebaser := &gittest.Rebaser{State: git.TreeState{Branch: "agent/a1"}}

			_, err := fixture.newService(NewRebaseServiceParams{
				Settler:  buildSettler(channel, observer),
				Notifier: fixture.newMailer(),
				Rebaser:  rebaser,
			}).Rebase(t.Context(), agent.ID)

			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, tt.wantCalls, rebaser.Calls)
			inbox := fixture.readInbox(t, agent.ID)
			if tt.wantMail == "" {
				assert.Empty(t, inbox)

				return
			}
			require.Len(t, inbox, 1)
			assert.Contains(t, inbox[0], tt.wantMail)
		})
	}
}

func holdWhileSettling(t *testing.T, fixture *rebaseFixture, id string) {
	_, err := fixture.stores.Agents.Hold(t.Context(), agentstore.HoldParams{ID: id, At: time.Now()})
	require.NoError(t, err)
}

// TestRebaseMailSaysWhatHappenedToTheBranch covers what the rebase mail tells the agent.
func TestRebaseMailSaysWhatHappenedToTheBranch(t *testing.T) {
	const park = "refs/weave/prerefresh/a1/1"

	tests := []struct {
		name        string
		result      RebaseResult
		wantSaid    string
		wantNotSaid string
	}{
		{
			name:     "a rebase stopped on conflicts",
			result:   RebaseResult{Conflicted: true, ParkedRef: park},
			wantSaid: park,
		},
		{
			name:        "an agent with no work of its own",
			result:      RebaseResult{ParkedRef: park},
			wantSaid:    "You had no commits of your own to replay",
			wantNotSaid: "of your commits are no longer on the branch",
		},
		{
			name: "a branch that reached the base without replaying anything",
			result: RebaseResult{
				RebasePlan: RebasePlan{Landed: 2},
				Moved:      true,
				ParkedRef:  park,
			},
			wantSaid: park,
		},
		{
			name:        "a replay that dropped commits the base already held, with no park to name",
			result:      RebaseResult{RebasePlan: RebasePlan{Landed: 2, Replay: 1}},
			wantSaid:    "2 of your commits are no longer on the branch",
			wantNotSaid: "A copy of the worktree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := rebaseMail(tt.result)

			assert.Contains(t, body, tt.wantSaid)
			if tt.wantNotSaid != "" {
				assert.NotContains(t, body, tt.wantNotSaid)
			}
		})
	}
}

// TestRebaseRefusesAnAgentInTheCallersCheckout covers a caller asking to rebase the checkout it works in.
func TestRebaseRefusesAnAgentInTheCallersCheckout(t *testing.T) {
	target := t.TempDir()
	linked := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(target, linked))

	tests := []struct {
		name string
		// askerCwd seeds the caller in that directory unless asker names one.
		askerCwd string
		asker    func(targetID string) caller.Caller
		wantErr  error
	}{
		{
			name: "the agent itself, whose turn the rebase would cut off",
			asker: func(targetID string) caller.Caller {
				return caller.Caller{AgentID: targetID}
			},
			wantErr: ErrRebaseSelf,
		},
		{
			name:     "an agent in the same checkout",
			askerCwd: target,
			wantErr:  ErrRebaseSelf,
		},
		{
			name:     "an agent in the same checkout spelled with a trailing slash",
			askerCwd: target + "/",
			wantErr:  ErrRebaseSelf,
		},
		{
			name:     "an agent in the same checkout spelled with a dot segment",
			askerCwd: target + "/./",
			wantErr:  ErrRebaseSelf,
		},
		{
			name:     "an agent in the same checkout reached through a symlink",
			askerCwd: linked,
			wantErr:  ErrRebaseSelf,
		},
		{
			name:     "an agent working somewhere else",
			askerCwd: t.TempDir(),
		},
		{
			name: "a person, who carries no agent id",
			asker: func(string) caller.Caller {
				return caller.Caller{}
			},
		},
		{
			name: "an asker weave has no row for at all",
			asker: func(string) caller.Caller {
				id, err := store.NewID()
				require.NoError(t, err)

				return caller.Caller{AgentID: id}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{Cwd: target})
			var from caller.Caller
			if tt.asker != nil {
				from = tt.asker(agent.ID)
			} else {
				asker := fixture.seed(t, agentstore.Agent{
					Name:  "asker",
					Cwd:   tt.askerCwd,
					State: agentstore.StateThinking,
				})
				from = caller.Caller{AgentID: asker.ID}
			}
			rebaser := &gittest.Rebaser{State: git.TreeState{Branch: "fix/auth"}}

			_, err := fixture.newService(NewRebaseServiceParams{Rebaser: rebaser}).Rebase(caller.With(t.Context(), from), agent.ID)

			if tt.wantErr == nil {
				require.NoError(t, err)
				assert.Equal(t, []string{"park", "rebase"}, rebaser.Calls)

				return
			}

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, rebaser.Calls)
		})
	}
}
