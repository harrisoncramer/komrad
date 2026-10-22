package checkouts

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"komrad/internal/caller"
	"komrad/internal/events/eventstest"
	"komrad/internal/forge"
	"komrad/internal/git"
	"komrad/internal/gittest"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	mailstore "komrad/internal/store/mail"
	taskstore "komrad/internal/store/task"
	workflowstore "komrad/internal/store/workflow"
	"komrad/internal/storetest"
	"komrad/internal/tasks"
	"komrad/internal/waketest"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type rebaseFixture struct {
	stores          *storetest.Stores
	events          *eventstest.Queue
	workflowID      string
	checkoutSync    *waketest.Counter
	cascade         *waketest.Counter
	pullRequestSync *waketest.Counter
	forges          *retargetingForge
}

// retargetingForge points each pull request at another base branch, recording it, and answers err.
type retargetingForge struct {
	forge.Forge
	err        error
	retargeted []retargeted
}

type retargeted struct {
	url  string
	base string
}

func (f *retargetingForge) ForAddress(context.Context, string) (string, forge.Forge, error) {
	return "github.com", f, nil
}

func (f *retargetingForge) SetBase(_ context.Context, url, base string) error {
	f.retargeted = append(f.retargeted, retargeted{url: url, base: base})

	return f.err
}

// TestRebaseRefusals covers every state of the task a rebase declines.
func TestRebaseRefusals(t *testing.T) {
	tests := []struct {
		name    string
		lay     func(t *testing.T, fixture *rebaseFixture) string
		wantErr error
	}{
		{
			name: "a task no id or slug names",
			lay: func(*testing.T, *rebaseFixture) string {
				return "no-such-task"
			},
			wantErr: store.ErrNotFound,
		},
		{
			name: "a task whose writer is still being set up",
			lay: func(t *testing.T, fixture *rebaseFixture) string {
				return *fixture.seed(t, agentstore.Agent{State: agentstore.StateSettingUp}).TaskID
			},
			wantErr: errBusy,
		},
		{
			name: "a branch a human made, whose history is not komrad's to rewrite",
			lay: func(t *testing.T, fixture *rebaseFixture) string {
				agent := fixture.seed(t, agentstore.Agent{})
				claimed, err := fixture.stores.Tasks.ClaimCheckout(t.Context(), taskstore.ClaimCheckoutParams{ID: *agent.TaskID, AdoptedBranch: "harry/spike"})
				require.NoError(t, err)
				require.True(t, claimed)

				return *agent.TaskID
			},
			wantErr: errRebaseAdopted,
		},
		{
			name: "a task komrad cut no checkout for",
			lay: func(t *testing.T, fixture *rebaseFixture) string {
				return fixture.stores.SeedTask(t, taskstore.CreateParams{WorkflowID: fixture.workflowID, Slug: "bare"}).Slug
			},
			wantErr: ErrNoWorktree,
		},
		{
			name: "a task whose directory is gone",
			lay: func(t *testing.T, fixture *rebaseFixture) string {
				agent := fixture.seed(t, agentstore.Agent{})
				fixture.stores.SeedTaskHead(t, agent.ID, taskstore.SetHeadParams{WorktreeRemovalReason: taskstore.WorktreeGone})

				return *agent.TaskID
			},
			wantErr: ErrNoWorktree,
		},
		{
			name: "a task being deleted",
			lay: func(t *testing.T, fixture *rebaseFixture) string {
				agent := fixture.seed(t, agentstore.Agent{})
				require.NoError(t, fixture.stores.Tasks.MarkDeleting(t.Context(), []string{*agent.TaskID}))

				return *agent.TaskID
			},
			wantErr: ErrNoWorktree,
		},
		{
			name: "a task in a workflow that makes no worktrees",
			lay: func(t *testing.T, fixture *rebaseFixture) string {
				flow := fixture.stores.SeedWorkflow(t, workflowstore.Config{Name: "no-worktrees"})

				return *fixture.seed(t, agentstore.Agent{WorkflowID: flow.ID}).TaskID
			},
			wantErr: ErrNoWorktree,
		},
		{
			name: "a writer part way through a turn, before anything moves the branch",
			lay: func(t *testing.T, fixture *rebaseFixture) string {
				return *fixture.seed(t, agentstore.Agent{Pane: "%1", State: agentstore.StateThinking}).TaskID
			},
			wantErr: errWriterNotAtRest,
		},
		{
			name: "a task whose tree something else is working in",
			lay: func(t *testing.T, fixture *rebaseFixture) string {
				return *fixture.seed(t, agentstore.Agent{Cwd: "/locked"}).TaskID
			},
			wantErr: errBusy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			ref := tt.lay(t, fixture)
			locks := NewTreeLocks()
			release, taken := locks.TryLock("/locked")
			require.True(t, taken)
			t.Cleanup(release)
			rebaser := &gittest.Mover{State: git.TreeState{Branch: "agent/a1"}}

			_, err := fixture.newService(NewServiceParams{Git: rebaser, Locks: locks}).Rebase(t.Context(), RebaseParams{Task: ref})

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, rebaser.Calls)
			assert.Zero(t, fixture.checkoutSync.Wakes, "nothing moved, so nothing needs reading back")
		})
	}
}

// TestRebaseWithNoWriter covers a task whose writer is gone being rebased by its slug, with nobody
// to tell.
func TestRebaseWithNoWriter(t *testing.T) {
	fixture := newRebaseFixture(t)
	agent := fixture.seed(t, agentstore.Agent{Pane: "%1"})
	_, err := fixture.stores.Agents.Archive(t.Context(), []string{agent.ID})
	require.NoError(t, err)
	task, err := fixture.stores.Tasks.Get(t.Context(), *agent.TaskID)
	require.NoError(t, err)
	rebaser := &gittest.Mover{State: git.TreeState{Branch: "agent/a1"}}

	_, err = fixture.newService(NewServiceParams{Git: rebaser}).Rebase(t.Context(), RebaseParams{Task: task.Slug})
	require.NoError(t, err)

	assert.Equal(t, []string{"park", "rebase"}, rebaser.Calls)
	assert.Empty(t, fixture.readInbox(t, agent.ID), "the archived agent is not the writer, so it is told nothing")
	assert.Contains(t, rebaser.Parked[0], "/"+task.ID+"/", "the park is kept under the task")
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

// TestRebaseRefusesATreeGitWillNotMove covers the checkout's own refusals.
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

			_, err := fixture.newService(NewServiceParams{Git: &gittest.Mover{State: tt.state}}).Rebase(t.Context(), RebaseParams{Task: *agent.TaskID})

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestRebaseMovesTheBranch covers the git calls a rebase makes and what reaches the row.
func TestRebaseMovesTheBranch(t *testing.T) {
	tests := []struct {
		name        string
		rebaser     *gittest.Mover
		wantCalls   []string
		wantCutoff  string
		wantStopped bool
	}{
		{
			name: "a rebase stopped on conflicts",
			rebaser: &gittest.Mover{
				State:     git.TreeState{Branch: "agent/a1"},
				ReplayErr: git.ErrRebaseConflicted,
			},
			wantCalls:   []string{"park", "rebase"},
			wantStopped: true,
		},
		{
			name: "a squash-merged branch replays from the cutoff, since a plain rebase would collide with the landed content",
			rebaser: &gittest.Mover{
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
			rebaser: &gittest.Mover{
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

			result, err := fixture.newService(NewServiceParams{Git: tt.rebaser}).Rebase(t.Context(), RebaseParams{Task: *agent.TaskID})
			require.NoError(t, err)

			assert.Equal(t, tt.wantCalls, tt.rebaser.Calls)
			assert.Len(t, tt.rebaser.Parked, 1)
			assert.Equal(t, tt.wantStopped, result.Conflicted)
			assert.Equal(t, tt.wantCutoff, result.Cutoff)
			assert.Equal(t, tt.wantCutoff, tt.rebaser.Cutoff)
			assert.Equal(t, tt.rebaser.Prediction.Replay, result.Replay)
			assert.False(t, result.Moved, "work was replayed or stopped, so the branch was not merely moved")

			assert.Equal(t, tt.wantStopped, fixture.readCheckout(t, agent).Sequence.IsInProgress())
			assert.Equal(t, 1, fixture.checkoutSync.Wakes)
		})
	}
}

// TestRebaseOntoBaseClearsTheParentOnceTheRetargetFinishes covers re-targeting a child onto the workflow's base branch.
func TestRebaseOntoBaseClearsTheParentOnceTheRetargetFinishes(t *testing.T) {
	tests := []struct {
		name       string
		replayErr  error
		wantErr    bool
		wantParent bool
	}{
		{
			name:       "a re-target that fails keeps the parent",
			replayErr:  errors.New("the post-rewrite hook failed"),
			wantErr:    true,
			wantParent: true,
		},
		{
			name:       "a re-target stopped on conflicts keeps the parent until the branch leaves it",
			replayErr:  git.ErrRebaseConflicted,
			wantParent: true,
		},
		{
			name: "a re-target that finishes clears the parent and leaves the pull request until origin holds the branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newRebaseFixture(t)
			laid := fixture.seedTaskOnParent(t, agentstore.Agent{})
			seedChildPullRequest(t, fixture, laid)
			rebaser := newChildGit(laid.parent.GetBranch(), "newtip")
			rebaser.Prediction = git.Prediction{Replay: 2}
			rebaser.ReplayErr = tt.replayErr

			_, err := fixture.newService(NewServiceParams{Git: rebaser}).Rebase(t.Context(), RebaseParams{Task: laid.task, OntoBase: true})
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			got, err := fixture.stores.Tasks.Get(t.Context(), laid.task)
			require.NoError(t, err)
			assert.Equal(t, tt.wantParent, got.Parent != nil)
			assert.Empty(t, fixture.forges.retargeted)
		})
	}
}

// TestRebaseWakesTheCheckoutSyncWhenTheReplayFails covers a replay that failed part-way.
func TestRebaseWakesTheCheckoutSyncWhenTheReplayFails(t *testing.T) {
	fixture := newRebaseFixture(t)
	agent := fixture.seed(t, agentstore.Agent{})
	rebaser := &gittest.Mover{
		State:      git.TreeState{Branch: "agent/a1"},
		Prediction: git.Prediction{Replay: 1},
		ReplayErr:  errors.New("the post-rewrite hook failed"),
	}

	_, err := fixture.newService(NewServiceParams{Git: rebaser}).Rebase(t.Context(), RebaseParams{Task: *agent.TaskID})

	require.Error(t, err)
	assert.Equal(t, 1, fixture.checkoutSync.Wakes)
}

// TestRebaseRefusesATaskInTheCallersCheckout covers a caller asking to rebase the checkout it works in.
func TestRebaseRefusesATaskInTheCallersCheckout(t *testing.T) {
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
			name: "the writer itself, whose turn the rebase would cut off",
			asker: func(targetID string) caller.Caller {
				return caller.Caller{AgentID: targetID}
			},
			wantErr: errSelf,
		},
		{
			name:     "an agent in the same checkout",
			askerCwd: target,
			wantErr:  errSelf,
		},
		{
			name:     "an agent in the same checkout spelled with a trailing slash",
			askerCwd: target + "/",
			wantErr:  errSelf,
		},
		{
			name:     "an agent in the same checkout spelled with a dot segment",
			askerCwd: target + "/./",
			wantErr:  errSelf,
		},
		{
			name:     "an agent in the same checkout reached through a symlink",
			askerCwd: linked,
			wantErr:  errSelf,
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
			rebaser := &gittest.Mover{State: git.TreeState{Branch: "fix/auth"}}

			_, err := fixture.newService(NewServiceParams{Git: rebaser}).Rebase(caller.With(t.Context(), from), RebaseParams{Task: *agent.TaskID})

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
			rebaser := &gittest.Mover{State: git.TreeState{Branch: "fix/auth"}}

			_, err := fixture.newService(NewServiceParams{Git: rebaser}).Rebase(caller.With(t.Context(), tt.asker(t)), RebaseParams{Task: *agent.TaskID})
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
		cascade:         &waketest.Counter{},
		pullRequestSync: &waketest.Counter{},
		forges:          &retargetingForge{},
		events:          eventstest.NewQueue(t, stores, NewEventHandler(), NewPushedHandler(), NewAbortedHandler(), NewCascadedHandler()),
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

	seeded := f.stores.SeedAgent(t, agent)
	require.NoError(t, f.stores.Tasks.SetCheckoutDir(t.Context(), *seeded.TaskID, seeded.Cwd))

	return seeded
}

// readCheckout returns the checkout of the agent's task.
func (f *rebaseFixture) readCheckout(t *testing.T, agent *agentstore.Agent) taskstore.Checkout {
	t.Helper()

	task, err := f.stores.Tasks.Get(t.Context(), *agent.TaskID)
	require.NoError(t, err)

	return task.Checkout
}

// newService builds the service over the real stores.
func (f *rebaseFixture) newService(params NewServiceParams) *Service {
	params.Agents = f.stores.Agents
	params.Tasks = f.stores.Tasks
	params.TaskFinder = tasks.NewTaskService(tasks.NewTaskServiceParams{Tasks: f.stores.Tasks})
	params.Tx = f.stores.Pool
	params.Workflows = f.stores.Workflows
	if params.Events == nil {
		params.Events = f.events
	}
	if params.CheckoutSync == nil {
		params.CheckoutSync = f.checkoutSync
	}
	if params.Cascade == nil {
		params.Cascade = f.cascade
	}
	if params.Git == nil {
		params.Git = &gittest.Mover{
			State:    git.TreeState{Branch: "agent/a1"},
			Own:      1,
			Sequence: git.SequenceRebase,
		}
	}
	if params.PullRequestSync == nil {
		params.PullRequestSync = f.pullRequestSync
	}
	params.PullRequests = f.stores.PullRequests
	if params.Forges == nil {
		params.Forges = f.forges
	}

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

// TestALateRebaseRetryTellsTheLatestRebase pins an older rebase's event routed after a newer rebase telling the agent what the newer one did.
func TestALateRebaseRetryTellsTheLatestRebase(t *testing.T) {
	fixture := newRebaseFixture(t)
	agent := fixture.seed(t, agentstore.Agent{Pane: "%1"})
	queue := fixture.events
	service := fixture.newService(NewServiceParams{})
	target, err := service.rebaseEligible(t.Context(), RebaseParams{Task: *agent.TaskID})
	require.NoError(t, err)

	require.NoError(t, service.recordRebase(t.Context(), *target, rebaseOutcome{Result: RebaseResult{Conflicted: true}}))
	require.NoError(t, service.recordRebase(t.Context(), *target, rebaseOutcome{Result: RebaseResult{RebasePlan: RebasePlan{Landed: 2}, Moved: true}}))
	stale := queue.ListEvents()[0]
	require.NoError(t, queue.InsertTx(t.Context(), stale))

	inbox := fixture.readInbox(t, agent.ID)
	require.Len(t, inbox, 1)
	assert.NotContains(t, inbox[0], "hit conflicts")
	assert.Contains(t, inbox[0], "already been merged")
}
