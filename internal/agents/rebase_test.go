package agents

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/caller"
	"weave/internal/git"
	"weave/internal/harness"
	"weave/internal/store"

	agentstore "weave/internal/store/agent"
	mailstore "weave/internal/store/mail"
	workflowstore "weave/internal/store/workflow"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRebaseRefusals covers every state a rebase declines, which is most of the feature: the
// operation itself is one git command and the judgement is all in what it will not touch.
func TestRebaseRefusals(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name string
		// agent is the row, with the id, name, workflow and directory filled in below so each
		// case names only what it is about.
		agent agentstore.Agent
		// noWorktreeWorkflow turns the workflow's worktree toggle off, which is one of several
		// distinct routes to ErrRebaseNoWorktree and has to be asked for rather than inferred
		// from the wanted error: the child and provisioner cases want that error with the
		// toggle on, and inferring it would pass them for the wrong reason.
		noWorktreeWorkflow bool
		wantErr            error
	}{
		{
			name:    "an agent part-way through a turn, since the files would move under it",
			agent:   agentstore.Agent{State: agentstore.StateThinking},
			wantErr: ErrRebaseBusy,
		},
		{
			name:    "an agent waiting on a question, which is also an unfinished turn",
			agent:   agentstore.Agent{State: agentstore.StateWaiting},
			wantErr: ErrRebaseBusy,
		},
		{
			name:    "an agent whose pane could not be read, which says nothing about whether it is working",
			agent:   agentstore.Agent{State: agentstore.StateUncertain, Pane: "%1"},
			wantErr: ErrRebaseBusy,
		},
		{
			name:    "an agent whose worktree is still being provisioned",
			agent:   agentstore.Agent{State: agentstore.StateSettingUp},
			wantErr: ErrRebaseBusy,
		},
		{
			name:    "a branch a human made, whose history is not weave's to rewrite",
			agent:   agentstore.Agent{State: agentstore.StateIdle, AdoptedBranch: "harry/spike"},
			wantErr: ErrRebaseAdopted,
		},
		{
			name: "a child, since rebasing one rewrites its parent's branch",
			agent: agentstore.Agent{
				State:         agentstore.StateIdle,
				ParentAgentID: xptr.Ptr("parent"),
			},
			wantErr: ErrRebaseNoWorktree,
		},
		{
			name: "an agent told to keep the directory it was given",
			agent: agentstore.Agent{
				State:            agentstore.StateIdle,
				ProvisionEnabled: xptr.Ptr(false),
			},
			wantErr: ErrRebaseNoWorktree,
		},
		{
			name:    "an agent whose directory is gone",
			agent:   agentstore.Agent{State: agentstore.StateIdle, WorktreeRemoved: true},
			wantErr: ErrRebaseNoWorktree,
		},
		{
			name:               "an agent in a workflow that makes no worktrees",
			agent:              agentstore.Agent{State: agentstore.StateIdle},
			noWorktreeWorkflow: true,
			wantErr:            ErrRebaseNoWorktree,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agent := tt.agent
			agent.ID = "a1"
			agent.Name = "auth"
			agent.WorkflowID = "w1"
			if agent.Cwd == "" {
				agent.Cwd = "/tree"
			}

			flow := worktreeFlow()
			flow.Worktree.Enabled = !tt.noWorktreeWorkflow

			service := NewRebaseService(NewRebaseServiceParams{
				Agents:    fakeRebaseAgents{agent: agent},
				Workflows: fakeRebaseWorkflows{flow: flow},
				Rebaser:   &fakeRebaser{},
			})

			_, err := service.Rebase(t.Context(), "a1")
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// TestRebaseRefusesAnArchivedAgent keeps the archived case beside the others rather than in the
// table, since it is the one refusal that is not about the worktree.
func TestRebaseRefusesAnArchivedAgent(t *testing.T) {
	t.Parallel()

	at := time.Now()
	service := NewRebaseService(NewRebaseServiceParams{
		Agents: fakeRebaseAgents{agent: agentstore.Agent{
			ID:         "a1",
			Name:       "auth",
			WorkflowID: "w1",
			Cwd:        "/tree",
			State:      agentstore.StateIdle,
			ArchivedAt: &at,
		}},
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Rebaser:   &fakeRebaser{},
	})

	_, err := service.Rebase(t.Context(), "a1")
	require.ErrorIs(t, err, ErrAgentArchived)
}

// TestRebaseTakesTheTreeLock pins the lock being keyed on the directory rather than the agent,
// which is what makes it cover a child sharing its parent's checkout.
func TestRebaseTakesTheTreeLock(t *testing.T) {
	t.Parallel()

	locks := NewTreeLocks()
	release, taken := locks.TryLock("/tree")
	require.True(t, taken)
	defer release()

	service := NewRebaseService(NewRebaseServiceParams{
		Agents: fakeRebaseAgents{agent: agentstore.Agent{
			ID:         "a1",
			Name:       "auth",
			WorkflowID: "w1",
			Cwd:        "/tree",
			State:      agentstore.StateIdle,
		}},
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Locks:     locks,
		Rebaser:   &fakeRebaser{},
	})

	_, err := service.Rebase(t.Context(), "a1")
	require.ErrorIs(t, err, ErrRebaseBusy)
}

// TestTreeLocksHoldOnePathAtATime covers the lock itself, since two agents in different trees
// must not block each other.
func TestTreeLocksHoldOnePathAtATime(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	release, taken := NewTreeLocks().TryLock("")

	assert.False(t, taken, "an empty path is refused")
	assert.Nil(t, release)
}

func worktreeFlow() workflowstore.Workflow {
	flow := workflowstore.Workflow{ID: "w1"}
	flow.Worktree.Enabled = true

	return flow
}

type fakeRebaseAgents struct {
	agent agentstore.Agent
}

func (s fakeRebaseAgents) GetByID(_ context.Context, _ string) (*agentstore.Agent, error) {
	agent := s.agent

	return &agent, nil
}

func (s fakeRebaseAgents) SetMetadata(context.Context, agentstore.SetMetadataParams) (bool, error) {
	return true, nil
}

func (s fakeRebaseAgents) SetSequenceInProgressByID(context.Context, string, bool) error {
	return nil
}

type fakeRebaseWorkflows struct {
	flow workflowstore.Workflow
}

func (s fakeRebaseWorkflows) Get(_ context.Context, _ string) (*workflowstore.Workflow, error) {
	flow := s.flow

	return &flow, nil
}

func (s fakeRebaseWorkflows) GetIncludingArchived(ctx context.Context, id string) (*workflowstore.Workflow, error) {
	return s.Get(ctx, id)
}

// TestRebaseRecordsAStoppedRebase runs the real service against a real worktree, because the
// decision it pins is which path writes the stopped rebase onto the row and a helper mirroring
// that decision in the test would assert nothing.
func TestRebaseRecordsAStoppedRebase(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name        string
		conflict    bool
		wantStopped bool
	}{
		{
			name:        "a stopped rebase reaches the row without waiting for the next pass",
			conflict:    true,
			wantStopped: true,
		},
		{
			name:        "a completed rebase leaves the row alone",
			conflict:    false,
			wantStopped: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			agents := &recordingRebaseAgents{agent: agentstore.Agent{
				ID:         "a1",
				Name:       "auth",
				WorkflowID: "w1",
				Cwd:        "/tree",
				State:      agentstore.StateIdle,
			}}

			rebaser := &fakeRebaser{state: git.TreeState{Branch: "agent/a1"}}
			if tt.conflict {
				rebaser.replayErr = git.ErrRebaseConflicted
			}

			service := NewRebaseService(NewRebaseServiceParams{
				Agents:    agents,
				Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
				Rebaser:   rebaser,
			})

			result, err := service.Rebase(t.Context(), "a1")
			require.NoError(t, err)
			require.Equal(t, tt.conflict, result.Conflicted)

			assert.Equal(t, tt.wantStopped, agents.setStopped)
		})
	}
}

// fakeRebaser answers as a checkout with no repository behind it.
type fakeRebaser struct {
	state      git.TreeState
	prediction git.Prediction
	commits    int
	parked     []string
	cutoff     string
	// replayErr is what a replay answers with, which is how a conflict is asked for.
	replayErr error
	// calls is what was asked of git, in the order it was asked.
	calls []string
}

func (f *fakeRebaser) ReadTreeState(context.Context, string) (git.TreeState, error) {
	return f.state, nil
}

func (f *fakeRebaser) FetchBase(_ context.Context, _, _ string) (ref, sha string, err error) {
	return "origin/main", "basesha", nil
}

func (f *fakeRebaser) CountCommitsToReplay(context.Context, string, string) (int, error) {
	return f.commits, nil
}

func (f *fakeRebaser) PredictRebase(context.Context, string, string) (git.Prediction, error) {
	return f.prediction, nil
}

func (f *fakeRebaser) Park(_ context.Context, _, ref string) (string, error) {
	f.parked = append(f.parked, ref)
	f.calls = append(f.calls, "park")

	return "parkedsha", nil
}

func (f *fakeRebaser) ReleaseRefsUnder(context.Context, string, string, string) error {
	return nil
}

func (f *fakeRebaser) Rebase(context.Context, string, string) error {
	f.calls = append(f.calls, "rebase")

	return f.replayErr
}

func (f *fakeRebaser) RebaseOnto(_ context.Context, _, _, cutoff string) error {
	f.cutoff = cutoff
	f.calls = append(f.calls, "rebaseOnto")

	return f.replayErr
}

// recordingRebaseAgents notes whether the pull request key was cleared, which is the write the
// decision under test produces.
type recordingRebaseAgents struct {
	agent      agentstore.Agent
	setStopped bool
}

func (r *recordingRebaseAgents) GetByID(context.Context, string) (*agentstore.Agent, error) {
	agent := r.agent

	return &agent, nil
}

func (r *recordingRebaseAgents) SetSequenceInProgressByID(_ context.Context, _ string, inProgress bool) error {
	r.setStopped = inProgress

	return nil
}

// TestRebaseReplaysWhenTheTreesDiffer is the other side of that decision, since a fast-forward
// taken whenever a rebase looked awkward would throw work away.
func TestRebaseReplaysWhenTheTreesDiffer(t *testing.T) {
	t.Parallel()

	rebaser := &fakeRebaser{
		state:      git.TreeState{Branch: "agent/a1"},
		prediction: git.Prediction{Replay: 1},
	}

	service := NewRebaseService(NewRebaseServiceParams{
		Agents: &recordingRebaseAgents{agent: agentstore.Agent{
			ID:         "a1",
			Name:       "auth",
			WorkflowID: "w1",
			Cwd:        "/tree",
			State:      agentstore.StateIdle,
		}},
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Rebaser:   rebaser,
	})

	result, err := service.Rebase(t.Context(), "a1")
	require.NoError(t, err)

	assert.False(t, result.Moved, "work was replayed, so the branch was not merely moved")
	assert.Empty(t, result.Cutoff)
	assert.Equal(t, []string{"park", "rebase"}, rebaser.calls, "no cutoff, so the whole branch replays")
}

// TestRebaseRefusesAPathTheIndexIsHolding covers a path whose working copy git is deliberately
// not reading, which nothing could put back if the move went wrong.
func TestRebaseRefusesAPathTheIndexIsHolding(t *testing.T) {
	t.Parallel()

	service := NewRebaseService(NewRebaseServiceParams{
		Agents: &recordingRebaseAgents{agent: agentstore.Agent{
			ID:         "a1",
			Name:       "auth",
			WorkflowID: "w1",
			Cwd:        "/tree",
			State:      agentstore.StateIdle,
		}},
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Rebaser: &fakeRebaser{state: git.TreeState{
			Branch:      "agent/a1",
			HiddenPaths: []string{"own.txt"},
		}},
	})

	_, err := service.Rebase(t.Context(), "a1")
	require.ErrorIs(t, err, ErrRebaseHiddenPaths)
}

// TestRebaseParksBeforeItAsksGitToMove pins the order the whole undo depends on.
func TestRebaseParksBeforeItAsksGitToMove(t *testing.T) {
	t.Parallel()

	rebaser := &fakeRebaser{state: git.TreeState{Branch: "agent/a1"}}

	service := NewRebaseService(NewRebaseServiceParams{
		Agents: &recordingRebaseAgents{agent: agentstore.Agent{
			ID:         "a1",
			Name:       "auth",
			WorkflowID: "w1",
			Cwd:        "/tree",
			State:      agentstore.StateIdle,
		}},
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Rebaser:   rebaser,
	})

	_, err := service.Rebase(t.Context(), "a1")
	require.NoError(t, err)

	require.Len(t, rebaser.parked, 1)
	assert.Equal(t, []string{"park", "rebase"}, rebaser.calls)
}

// TestRebaseSettlesTheAgentBeforeItMoves is the safety argument for the row being a pane reading
// seconds old: the pane is read as it is now, a turn in flight is cut, and nothing is parked or
// moved until the agent is seen to have come to rest.
func TestRebaseSettlesTheAgentBeforeItMoves(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
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
			wantErr:        ErrUnsettled,
			wantSaid:       "had not stopped",
			wantInterrupts: 1,
		},
		{
			name:       "a pane that could not be read",
			pane:       "%1",
			observeErr: errors.New("the pane is gone"),
			wantErr:    ErrUnsettled,
			wantSaid:   "could not be read",
		},
		{
			name:     "a frame that matched nothing, which is no evidence of rest",
			pane:     "%1",
			readings: []harness.Activity{harness.ActivityUnrecognized},
			wantErr:  ErrUnsettled,
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
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			channel := &fakeChannel{}
			observer := &settlingObserver{readings: tt.readings, err: tt.observeErr}
			rebaser := &treeReadRecorder{
				fakeRebaser: &fakeRebaser{state: git.TreeState{Branch: "agent/a1"}},
				observer:    observer,
			}

			service := NewRebaseService(NewRebaseServiceParams{
				Agents: fakeRebaseAgents{agent: agentstore.Agent{
					ID:         "a1",
					Name:       "auth",
					WorkflowID: "w1",
					Cwd:        "/tree",
					Pane:       tt.pane,
					State:      agentstore.StateIdle,
				}},
				Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
				Settler:   settlerOver(channel, observer),
				Rebaser:   rebaser,
			})

			_, err := service.Rebase(t.Context(), "a1")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				assert.Contains(t, err.Error(), tt.wantSaid)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantReads, observer.reads)
				assert.Equal(t, tt.wantReads, rebaser.readsBeforeTree, "the tree is read only once the agent has stopped")
			}

			assert.Equal(t, tt.wantInterrupts, channel.interrupts)
			assert.Equal(t, tt.wantCalls, rebaser.calls, "nothing is parked or moved under an agent still working")
		})
	}
}

// TestRebaseRefusesADirtyTreeWithoutCuttingTheTurn pins the tree being read before the agent is
// touched.
func TestRebaseRefusesADirtyTreeWithoutCuttingTheTurn(t *testing.T) {
	t.Parallel()

	channel := &fakeChannel{}
	observer := &settlingObserver{readings: []harness.Activity{harness.ActivityWorking}}

	service := NewRebaseService(NewRebaseServiceParams{
		Agents:    fakeRebaseAgents{agent: settledAgent()},
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Settler:   settlerOver(channel, observer),
		Rebaser:   &fakeRebaser{state: git.TreeState{Branch: "agent/a1", TrackedChanges: true}},
	})

	_, err := service.Rebase(t.Context(), "a1")
	require.ErrorIs(t, err, ErrRebaseDirty)

	assert.Zero(t, channel.interrupts, "a tree that cannot move costs the agent no turn")
	assert.Zero(t, observer.reads)
}

// TestRebaseTrustsTheSettledPaneOverTheRow covers the reconciler stamping the row thinking from a
// capture taken before the interrupt landed.
func TestRebaseTrustsTheSettledPaneOverTheRow(t *testing.T) {
	t.Parallel()

	stamped := settledAgent()
	stamped.State = agentstore.StateThinking

	channel := &fakeChannel{}
	observer := &settlingObserver{readings: []harness.Activity{harness.ActivityWorking, harness.ActivityIdle}}
	rebaser := &fakeRebaser{state: git.TreeState{Branch: "agent/a1"}}

	service := NewRebaseService(NewRebaseServiceParams{
		Agents:    &driftingRebaseAgents{first: settledAgent(), then: stamped},
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Settler:   settlerOver(channel, observer),
		Rebaser:   rebaser,
	})

	_, err := service.Rebase(t.Context(), "a1")
	require.NoError(t, err)

	assert.Equal(t, 1, channel.interrupts)
	assert.Equal(t, []string{"park", "rebase"}, rebaser.calls)
}

// TestRebaseTellsAnAgentCutForNothing covers a hold landing while the agent was settling, which
// refuses the rebase after its turn was already cut.
func TestRebaseTellsAnAgentCutForNothing(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		readings []harness.Activity
		wantMail int
	}{
		{
			name:     "an agent whose turn was cut is told",
			readings: []harness.Activity{harness.ActivityWorking, harness.ActivityIdle},
			wantMail: 1,
		},
		{
			name:     "an agent that was already at rest is left alone",
			readings: []harness.Activity{harness.ActivityIdle},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			held := settledAgent()
			held.HeldAt = xptr.Ptr(time.Now())

			channel := &fakeChannel{}
			notifier := &recordingNotifier{}
			rebaser := &fakeRebaser{state: git.TreeState{Branch: "agent/a1"}}

			service := NewRebaseService(NewRebaseServiceParams{
				Agents:    &driftingRebaseAgents{first: settledAgent(), then: held},
				Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
				Settler:   settlerOver(channel, &settlingObserver{readings: tt.readings}),
				Notifier:  notifier,
				Rebaser:   rebaser,
			})

			_, err := service.Rebase(t.Context(), "a1")
			require.ErrorIs(t, err, ErrAgentHeld)

			assert.Empty(t, rebaser.calls, "nothing moves under a held agent")
			require.Len(t, notifier.sent, tt.wantMail)
			if tt.wantMail > 0 {
				assert.Contains(t, notifier.sent[0], "interrupted")
			}
		})
	}
}

// settledAgent is an idle agent in a pane, on a worktree of its own.
func settledAgent() agentstore.Agent {
	return agentstore.Agent{
		ID:         "a1",
		Name:       "auth",
		WorkflowID: "w1",
		Cwd:        "/tree",
		Pane:       "%1",
		State:      agentstore.StateIdle,
	}
}

// driftingRebaseAgents answers the row as it stood first, then as it was left while the agent
// settled.
type driftingRebaseAgents struct {
	first agentstore.Agent
	then  agentstore.Agent
	reads int
}

func (d *driftingRebaseAgents) GetByID(context.Context, string) (*agentstore.Agent, error) {
	d.reads++
	agent := d.then
	if d.reads == 1 {
		agent = d.first
	}

	return &agent, nil
}

func (d *driftingRebaseAgents) SetSequenceInProgressByID(context.Context, string, bool) error {
	return nil
}

type recordingNotifier struct {
	sent []string
}

func (n *recordingNotifier) SendSystemMail(_ context.Context, _, body string) (*mailstore.Mail, error) {
	n.sent = append(n.sent, body)

	return &mailstore.Mail{}, nil
}

// treeReadRecorder notes how many pane readings had been taken by the time the tree was read.
type treeReadRecorder struct {
	*fakeRebaser
	observer        *settlingObserver
	readsBeforeTree int
}

func (r *treeReadRecorder) ReadTreeState(ctx context.Context, dir string) (git.TreeState, error) {
	r.readsBeforeTree = r.observer.reads

	return r.fakeRebaser.ReadTreeState(ctx, dir)
}

// TestRebaseReplaysFromTheCutoffThePlanFound covers the branch a squash merge needs, where
// replaying every commit would collide with content the base already holds.
func TestRebaseReplaysFromTheCutoffThePlanFound(t *testing.T) {
	t.Parallel()

	rebaser := &fakeRebaser{
		state: git.TreeState{Branch: "agent/a1"},
		prediction: git.Prediction{
			Cutoff: "cutoffsha",
			Landed: 2,
			Replay: 1,
		},
	}

	service := NewRebaseService(NewRebaseServiceParams{
		Agents: &recordingRebaseAgents{agent: agentstore.Agent{
			ID:         "a1",
			Name:       "auth",
			WorkflowID: "w1",
			Cwd:        "/tree",
			State:      agentstore.StateIdle,
		}},
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Rebaser:   rebaser,
	})

	result, err := service.Rebase(t.Context(), "a1")
	require.NoError(t, err)

	require.False(t, result.Conflicted, "the cutoff is what keeps this off the conflict path")
	assert.Equal(t, "cutoffsha", result.Cutoff)
	assert.Equal(t, "cutoffsha", rebaser.cutoff, "the cutoff the plan found is the one git is given")
	assert.Equal(t, []string{"park", "rebaseOnto"}, rebaser.calls, "a plain rebase would collide with the landed content")
	assert.Equal(t, 1, result.Replay, "the plan's counts reach the result")
	assert.Positive(t, result.Landed)
	assert.False(t, result.Moved, "work was replayed, so this is not a bare move")
}

// TestRebaseMailsForAnAgentWithNoWorkOfItsOwn is the commonest press of all, where the agent is
// simply behind and must not be told its commits are gone when it never had any.
func TestRebaseMailsForAnAgentWithNoWorkOfItsOwn(t *testing.T) {
	t.Parallel()

	service := NewRebaseService(NewRebaseServiceParams{
		Agents: &recordingRebaseAgents{agent: agentstore.Agent{
			ID:         "a1",
			Name:       "auth",
			WorkflowID: "w1",
			Cwd:        "/tree",
			State:      agentstore.StateIdle,
		}},
		Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
		Rebaser:   &fakeRebaser{state: git.TreeState{Branch: "agent/a1"}},
	})

	result, err := service.Rebase(t.Context(), "a1")
	require.NoError(t, err)
	require.Zero(t, result.Landed, "a branch that never had work of its own")

	assert.False(t, result.Moved, "nothing landed, so nothing was moved off the branch")
	body := rebaseMail(*result)
	assert.NotContains(t, body, "of your commits are no longer on the branch",
		"an agent with no commits must not be told its commits are gone")
	assert.Contains(t, body, "You had no commits of your own to replay")
}

// TestRebaseMailNamesThePark covers the undo being findable from every outcome, not only the one
// where the whole branch landed.
func TestRebaseMailNamesThePark(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		result RebaseResult
	}{
		{
			name:   "a rebase stopped on conflicts",
			result: RebaseResult{Conflicted: true, ParkedRef: "refs/weave/prerefresh/a1/1"},
		},
		{
			name: "a replay that dropped commits the base already held",
			result: RebaseResult{
				RebasePlan: RebasePlan{Landed: 2, Replay: 1},
				ParkedRef:  "refs/weave/prerefresh/a1/1",
			},
		},
		{
			name: "a branch that reached the base without replaying anything",
			result: RebaseResult{
				RebasePlan: RebasePlan{Landed: 2},
				Moved:      true,
				ParkedRef:  "refs/weave/prerefresh/a1/1",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Contains(t, rebaseMail(tt.result), "refs/weave/prerefresh/a1/1")
		})
	}
}

// TestReplayedMailSaysWhatLeftTheBranch covers the case this whole change exists for, where
// commits are dropped on purpose and the old wording said nothing was lost.
func TestReplayedMailSaysWhatLeftTheBranch(t *testing.T) {
	t.Parallel()

	body := rebaseMail(RebaseResult{
		RebasePlan: RebasePlan{Landed: 2, Replay: 1},
	})

	assert.Contains(t, body, "2 of your commits are no longer on the branch")
	assert.NotContains(t, body, "Nothing was lost")
}

// rebaseRoster answers each agent by its own id, which is what a caller sharing a checkout with
// another agent needs.
type rebaseRoster map[string]agentstore.Agent

// errRosterDown is the store failing to answer, which must not read as a caller working
// elsewhere.
var errRosterDown = errors.New("database is down")

func (r rebaseRoster) GetByID(_ context.Context, id string) (*agentstore.Agent, error) {
	if id == "a4" {
		return nil, errRosterDown
	}

	found, ok := r[id]
	if !ok {
		return nil, store.ErrNotFound
	}

	return &found, nil
}

func (r rebaseRoster) SetSequenceInProgressByID(context.Context, string, bool) error {
	return nil
}

// TestRebaseRefusesAnAgentInTheCallersCheckout covers who is asking rather than who the row
// belongs to, since a rebase rewrites the branch under everybody working in the directory and a
// child shares its parent's.
func TestRebaseRefusesAnAgentInTheCallersCheckout(t *testing.T) {
	t.Parallel()

	target := agentstore.Agent{
		ID: "a1", Name: "auth", WorkflowID: "w1", Cwd: "/tree", State: agentstore.StateIdle,
	}
	reviewer := agentstore.Agent{
		ID: "a2", Name: "auth-reviewer", WorkflowID: "w1", Cwd: "/tree", State: agentstore.StateThinking,
		ParentAgentID: xptr.Ptr("a1"),
	}
	elsewhere := agentstore.Agent{
		ID: "a3", Name: "billing", WorkflowID: "w1", Cwd: "/other", State: agentstore.StateThinking,
	}

	for _, tt := range []struct {
		name    string
		from    caller.Caller
		wantErr error
	}{
		{
			name:    "the agent itself, whose turn the rebase would cut off",
			from:    caller.Caller{AgentID: "a1"},
			wantErr: ErrRebaseSelf,
		},
		{
			name:    "its reviewer, which runs in the same checkout",
			from:    caller.Caller{AgentID: "a2"},
			wantErr: ErrRebaseSelf,
		},
		{
			name: "an agent working somewhere else",
			from: caller.Caller{AgentID: "a3"},
		},
		{
			name: "a person, who carries no agent id",
		},
		{
			name:    "an asker whose row could not be read, which is no answer either way",
			from:    caller.Caller{AgentID: "a4"},
			wantErr: errRosterDown,
		},
		{
			name: "an asker weave has no row for at all",
			from: caller.Caller{AgentID: "gone"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rebaser := &fakeRebaser{state: git.TreeState{Branch: "fix/auth"}}
			service := NewRebaseService(NewRebaseServiceParams{
				Agents:    rebaseRoster{"a1": target, "a2": reviewer, "a3": elsewhere},
				Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
				Rebaser:   rebaser,
			})

			_, err := service.Rebase(caller.With(t.Context(), tt.from), "a1")

			if tt.wantErr == nil {
				require.NoError(t, err)
				assert.Equal(t, []string{"park", "rebase"}, rebaser.calls)

				return
			}

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, rebaser.calls)
		})
	}
}

// TestRebaseRefusesTheCallerWhateverThePathSpelling covers a caller whose directory is the
// target's spelled another way, which is what an agent pointed at a path by hand holds.
func TestRebaseRefusesTheCallerWhateverThePathSpelling(t *testing.T) {
	t.Parallel()

	real := t.TempDir()
	linked := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(real, linked))

	for _, tt := range []struct {
		name      string
		targetCwd string
		askerCwd  string
		wantErr   error
	}{
		{
			name:      "a trailing slash",
			targetCwd: "/tree",
			askerCwd:  "/tree/",
			wantErr:   ErrRebaseSelf,
		},
		{
			name:      "a dot segment",
			targetCwd: "/tree",
			askerCwd:  "/tree/./",
			wantErr:   ErrRebaseSelf,
		},
		{
			name:      "a symlinked prefix",
			targetCwd: real,
			askerCwd:  linked,
			wantErr:   ErrRebaseSelf,
		},
		{
			name:      "another directory entirely",
			targetCwd: "/tree",
			askerCwd:  "/other",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			target := agentstore.Agent{
				ID: "a1", Name: "auth", WorkflowID: "w1", Cwd: tt.targetCwd, State: agentstore.StateIdle,
			}
			asker := agentstore.Agent{
				ID: "a2", Name: "billing", WorkflowID: "w1", Cwd: tt.askerCwd, State: agentstore.StateThinking,
			}

			rebaser := &fakeRebaser{state: git.TreeState{Branch: "fix/auth"}}
			service := NewRebaseService(NewRebaseServiceParams{
				Agents:    rebaseRoster{"a1": target, "a2": asker},
				Workflows: fakeRebaseWorkflows{flow: worktreeFlow()},
				Rebaser:   rebaser,
			})

			_, err := service.Rebase(caller.With(t.Context(), caller.Caller{AgentID: "a2"}), "a1")

			if tt.wantErr == nil {
				require.NoError(t, err)
				assert.Equal(t, []string{"park", "rebase"}, rebaser.calls)

				return
			}

			require.ErrorIs(t, err, tt.wantErr)
			assert.Empty(t, rebaser.calls)
		})
	}
}
