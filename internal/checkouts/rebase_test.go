package checkouts

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"komrad/internal/agents"
	"komrad/internal/caller"
	"komrad/internal/events/eventstest"
	"komrad/internal/git"
	"komrad/internal/gittest"
	"komrad/internal/harness"
	"komrad/internal/harnesstest"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/internal/waketest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rebaseFixture struct {
	stores          *storetest.Stores
	events          *eventstest.Queue
	workflowID      string
	checkoutSync    *waketest.Counter
	pullRequestSync *waketest.Counter
}

// TestRebaseRefusals covers every state of the row a rebase declines.
func TestRebaseRefusals(t *testing.T) {
	tests := []struct {
		name    string
		lay     func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent
		wantErr error
	}{
		{
			name: "an agent whose worktree is still being provisioned",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				return fixture.seed(t, agentstore.Agent{State: agentstore.StateSettingUp})
			},
			wantErr: ErrBusy,
		},
		{
			name: "a branch a human made, whose history is not komrad's to rewrite",
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
			wantErr: ErrNoWorktree,
		},
		{
			name: "an agent told to keep the directory it was given",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				return fixture.seed(t, agentstore.Agent{ProvisionEnabled: xptr.Ptr(false)})
			},
			wantErr: ErrNoWorktree,
		},
		{
			name: "an agent whose directory is gone",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				agent := fixture.seed(t, agentstore.Agent{})
				require.NoError(t, fixture.stores.Agents.SetCheckout(t.Context(), agent.ID, agentstore.SetCheckoutParams{WorktreeRemoved: true}))

				return agent
			},
			wantErr: ErrNoWorktree,
		},
		{
			name: "an agent in a workflow that makes no worktrees",
			lay: func(t *testing.T, fixture *rebaseFixture) *agentstore.Agent {
				flow := fixture.stores.SeedWorkflow(t, workflowstore.Config{Name: "no-worktrees"})

				return fixture.seed(t, agentstore.Agent{WorkflowID: flow.ID})
			},
			wantErr: ErrNoWorktree,
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
			wantErr: ErrBusy,
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

			_, err := fixture.newService(NewServiceParams{Rebaser: rebaser, Locks: locks}).Rebase(t.Context(), agent.ID)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, rebaser.Calls)
			assert.Zero(t, fixture.checkoutSync.Wakes, "nothing moved, so nothing needs reading back")
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
			observer := harnesstest.NewActivityObserver(harness.ActivityWorking)

			_, err := fixture.newService(NewServiceParams{
				Settler: buildSettler(channel, observer),
				Rebaser: &gittest.Rebaser{State: tt.state},
			}).Rebase(t.Context(), agent.ID)

			require.ErrorIs(t, err, tt.wantErr)
			assert.Zero(t, channel.Interrupts, "a tree that cannot move costs the agent no turn")
			assert.Zero(t, observer.Reads)
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

			result, err := fixture.newService(NewServiceParams{Rebaser: tt.rebaser}).Rebase(t.Context(), agent.ID)
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
			assert.Equal(t, tt.wantStopped, stored.Sequence.IsInProgress())
			assert.Equal(t, 1, fixture.checkoutSync.Wakes)
		})
	}
}

// TestRebaseWakesTheCheckoutSyncWhenTheReplayFails covers a replay that failed part-way.
func TestRebaseWakesTheCheckoutSyncWhenTheReplayFails(t *testing.T) {
	fixture := newRebaseFixture(t)
	agent := fixture.seed(t, agentstore.Agent{})
	rebaser := &gittest.Rebaser{
		State:      git.TreeState{Branch: "agent/a1"},
		Prediction: git.Prediction{Replay: 1},
		ReplayErr:  errors.New("the post-rewrite hook failed"),
	}

	_, err := fixture.newService(NewServiceParams{Rebaser: rebaser}).Rebase(t.Context(), agent.ID)

	require.Error(t, err)
	assert.Equal(t, 1, fixture.checkoutSync.Wakes)
}

// TestRebaseRefusesAnAgentThatWillNotSettle covers nothing being parked or moved until the pane is seen at rest.
func TestRebaseRefusesAnAgentThatWillNotSettle(t *testing.T) {
	tests := []struct {
		name           string
		readings       []harness.Activity
		observeErr     error
		wantSaid       string
		wantInterrupts int
	}{
		{
			name:           "an agent that will not stop",
			readings:       []harness.Activity{harness.ActivityWorking},
			wantSaid:       "had not stopped",
			wantInterrupts: 1,
		},
		{
			name:       "a pane that could not be read",
			observeErr: errors.New("the frame could not be captured"),
			wantSaid:   "could not be read",
		},
		{
			name:     "a frame that matched nothing, which is no evidence of rest",
			readings: []harness.Activity{harness.ActivityUnrecognized},
			wantSaid: "could not be read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{Pane: "%1"})
			channel := &harnesstest.Channel{}
			observer := harnesstest.NewActivityObserver(tt.readings...)
			observer.Err = tt.observeErr
			rebaser := &gittest.Rebaser{State: git.TreeState{Branch: "agent/a1"}}

			_, err := fixture.newService(NewServiceParams{
				Settler: buildSettler(channel, observer),
				Rebaser: rebaser,
			}).Rebase(t.Context(), agent.ID)

			require.ErrorIs(t, err, agents.ErrUnsettled)
			require.ErrorContains(t, err, tt.wantSaid)
			assert.Equal(t, tt.wantInterrupts, channel.Interrupts)
			assert.Empty(t, rebaser.Calls, "nothing is parked or moved under an agent still working")
		})
	}
}

// TestRebaseSettlesTheAgentBeforeItMoves covers the pane being read as it is now and a turn in flight being cut.
func TestRebaseSettlesTheAgentBeforeItMoves(t *testing.T) {
	tests := []struct {
		name           string
		pane           string
		state          agentstore.State
		readings       []harness.Activity
		prediction     git.Prediction
		wantInterrupts int
		wantReads      int
	}{
		{
			name:      "an agent at rest, which is read rather than interrupted",
			pane:      "%1",
			readings:  []harness.Activity{harness.ActivityIdle},
			wantReads: 1,
		},
		{
			name:           "an agent its row shows working, whose turn is cut",
			pane:           "%1",
			state:          agentstore.StateThinking,
			readings:       []harness.Activity{harness.ActivityWorking, harness.ActivityIdle},
			wantInterrupts: 1,
			wantReads:      2,
		},
		{
			name:           "an agent part-way through a turn its row does not show yet",
			pane:           "%1",
			readings:       []harness.Activity{harness.ActivityWorking, harness.ActivityWorking, harness.ActivityIdle},
			wantInterrupts: 1,
			wantReads:      3,
		},
		{
			name:     "an agent with no pane, which has nothing running",
			readings: []harness.Activity{harness.ActivityWorking},
		},
		{
			name:       "a working agent whose branch already holds the base, which is left alone",
			pane:       "%1",
			state:      agentstore.StateThinking,
			readings:   []harness.Activity{harness.ActivityWorking},
			prediction: git.Prediction{Contained: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{Pane: tt.pane, State: tt.state})
			channel := &harnesstest.Channel{}
			observer := harnesstest.NewActivityObserver(tt.readings...)
			readsBeforeTree := 0
			rebaser := &gittest.Rebaser{
				State:          git.TreeState{Branch: "agent/a1"},
				Prediction:     tt.prediction,
				BeforeReadTree: func() { readsBeforeTree = observer.Reads },
			}

			result, err := fixture.newService(NewServiceParams{
				Settler: buildSettler(channel, observer),
				Rebaser: rebaser,
			}).Rebase(t.Context(), agent.ID)
			require.NoError(t, err)

			assert.Equal(t, tt.wantInterrupts > 0, result.Interrupted)
			assert.Equal(t, tt.wantReads, observer.Reads)
			assert.Equal(t, tt.wantReads, readsBeforeTree, "the plan reads the tree only once the agent has stopped")
			assert.Equal(t, tt.wantInterrupts, channel.Interrupts)
			if tt.prediction.Contained {
				assert.Empty(t, rebaser.Calls)
				assert.Empty(t, fixture.readInbox(t, agent.ID))

				return
			}
			assert.Equal(t, []string{"park", "rebase"}, rebaser.Calls)
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
		{
			name: "a row stamped thinking after the interrupt",
			settling: func(t *testing.T, fixture *rebaseFixture, id string) {
				require.NoError(t, fixture.stores.Agents.SetStateByID(t.Context(), id, agentstore.StateThinking))
			},
			readings:  []harness.Activity{harness.ActivityWorking, harness.ActivityIdle},
			wantCalls: []string{"park", "rebase"},
			wantMail:  "Your branch is still the same branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{Pane: "%1"})
			channel := &harnesstest.Channel{}
			observer := harnesstest.NewActivityObserver(tt.readings...)
			observer.BeforeFirst = func() { tt.settling(t, fixture, agent.ID) }
			rebaser := &gittest.Rebaser{State: git.TreeState{Branch: "agent/a1"}}

			_, err := fixture.newService(NewServiceParams{
				Settler: buildSettler(channel, observer),
				Rebaser: rebaser,
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

// TestRebaseMailSaysWhatHappenedToTheBranch covers what the rebase mail tells the agent.
func TestRebaseMailSaysWhatHappenedToTheBranch(t *testing.T) {
	const park = "refs/komrad/prerefresh/a1/1"

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
		{
			name:     "a replay that cut the agent's turn",
			result:   RebaseResult{Interrupted: true},
			wantSaid: "pick up where you were",
		},
		{
			name:        "a conflict that cut the agent's turn, which waits rather than carrying on",
			result:      RebaseResult{Conflicted: true, Interrupted: true},
			wantSaid:    "Your turn was interrupted",
			wantNotSaid: "pick up where you were",
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
			wantErr: ErrSelf,
		},
		{
			name:     "an agent in the same checkout",
			askerCwd: target,
			wantErr:  ErrSelf,
		},
		{
			name:     "an agent in the same checkout spelled with a trailing slash",
			askerCwd: target + "/",
			wantErr:  ErrSelf,
		},
		{
			name:     "an agent in the same checkout spelled with a dot segment",
			askerCwd: target + "/./",
			wantErr:  ErrSelf,
		},
		{
			name:     "an agent in the same checkout reached through a symlink",
			askerCwd: linked,
			wantErr:  ErrSelf,
		},
		{
			name:     "an agent working somewhere else",
			askerCwd: t.TempDir(),
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

			_, err := fixture.newService(NewServiceParams{Rebaser: rebaser}).Rebase(caller.With(t.Context(), from), agent.ID)

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

// TestRebaseLetsThroughAnAskerWithNoCheckout covers a caller the check has no directory to compare.
func TestRebaseLetsThroughAnAskerWithNoCheckout(t *testing.T) {
	tests := []struct {
		name  string
		asker func(t *testing.T) caller.Caller
	}{
		{
			name: "a person, who carries no agent id",
			asker: func(*testing.T) caller.Caller {
				return caller.Caller{}
			},
		},
		{
			name: "an asker komrad has no row for at all",
			asker: func(t *testing.T) caller.Caller {
				id, err := store.NewID()
				require.NoError(t, err)

				return caller.Caller{AgentID: id}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			agent := fixture.seed(t, agentstore.Agent{})
			rebaser := &gittest.Rebaser{State: git.TreeState{Branch: "fix/auth"}}

			_, err := fixture.newService(NewServiceParams{Rebaser: rebaser}).Rebase(caller.With(t.Context(), tt.asker(t)), agent.ID)
			require.NoError(t, err)

			assert.Equal(t, []string{"park", "rebase"}, rebaser.Calls)
		})
	}
}

func newRebaseFixture(t *testing.T) *rebaseFixture {
	t.Helper()

	stores := storetest.Open(t)

	return &rebaseFixture{
		stores:          stores,
		checkoutSync:    &waketest.Counter{},
		pullRequestSync: &waketest.Counter{},
		events:          eventstest.NewQueue(t, stores, NewEventHandler(), NewPushedHandler(), NewAbortedHandler()),
		workflowID: stores.SeedWorkflow(t, workflowstore.Config{
			Name:     "flow",
			Worktree: workflowstore.WorktreeConfig{Enabled: true, BaseBranch: "main"},
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

// newService builds the service over the real stores.
func (f *rebaseFixture) newService(params NewServiceParams) *Service {
	params.Agents = f.stores.Agents
	params.Workflows = f.stores.Workflows
	if params.Events == nil {
		params.Events = f.events
	}
	if params.CheckoutSync == nil {
		params.CheckoutSync = f.checkoutSync
	}
	if params.Rebaser == nil {
		params.Rebaser = &gittest.Rebaser{State: git.TreeState{Branch: "agent/a1"}}
	}
	if params.Pusher == nil {
		params.Pusher = &gittest.Pusher{State: git.TreeState{Branch: "agent/a1"}, Own: 1}
	}
	if params.Aborter == nil {
		params.Aborter = &gittest.Aborter{Sequence: git.SequenceRebase, Branch: "agent/a1"}
	}
	if params.PullRequestSync == nil {
		params.PullRequestSync = f.pullRequestSync
	}
	params.PullRequests = f.stores.PullRequests

	return NewService(params)
}

func (f *rebaseFixture) readInbox(t *testing.T, id string) []string {
	t.Helper()

	sent, err := f.stores.Mail.ListPage(t.Context(), id, 10, nil)
	require.NoError(t, err)

	return xslice.Map(sent, func(piece mailstore.Mail) string {
		return piece.Body
	})
}

func holdWhileSettling(t *testing.T, fixture *rebaseFixture, id string) {
	_, err := fixture.stores.Agents.Hold(t.Context(), agentstore.HoldParams{ID: id, At: time.Now()})
	require.NoError(t, err)
}

// settleStep is how far the fake clock moves between two readings of the pane.
const settleStep = 250 * time.Millisecond

// buildSettler builds a settler whose clock walks the budget by itself.
func buildSettler(channel harness.Channel, observer harness.Observer) *agents.Settler {
	at := time.Now()

	return agents.NewSettler(agents.NewSettlerParams{
		Channel:  channel,
		Observer: observer,
		Clock: func() time.Time {
			at = at.Add(settleStep)

			return at
		},
		Wait: func(time.Duration) <-chan time.Time {
			rested := make(chan time.Time, 1)
			rested <- at

			return rested
		},
	})
}

// TestALateRebaseRetryTellsTheLatestRebase pins an older rebase's event routed after a newer rebase telling the agent what the newer one did.
func TestALateRebaseRetryTellsTheLatestRebase(t *testing.T) {
	fixture := newRebaseFixture(t)
	agent := fixture.seed(t, agentstore.Agent{Pane: "%1"})
	queue := fixture.events
	service := fixture.newService(NewServiceParams{})

	require.NoError(t, service.recordRebase(t.Context(), agent.ID, rebaseOutcome{Failure: "the older rebase failed"}))
	require.NoError(t, service.recordRebase(t.Context(), agent.ID, rebaseOutcome{Result: &RebaseResult{RebasePlan: RebasePlan{Landed: 2}, Moved: true}}))
	stale := queue.ListEvents()[0]
	require.NoError(t, queue.InsertTx(t.Context(), stale))

	inbox := fixture.readInbox(t, agent.ID)
	require.Len(t, inbox, 1)
	assert.NotContains(t, inbox[0], "the older rebase failed")
	assert.Contains(t, inbox[0], "already been merged")
}
