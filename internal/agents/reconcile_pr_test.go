package agents

import (
	"context"
	"errors"
	"testing"
	"time"

	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePRFinder struct {
	url   string
	err   error
	calls int
}

func (f *fakePRFinder) FindPR(context.Context, string, string) (string, error) {
	f.calls++

	return f.url, f.err
}

func seedWithBranch(t *testing.T, st *testStore, name, pane, state, branch string) agentstore.Agent {
	t.Helper()
	ctx := t.Context()
	workflowID := seedWorkflow(t, st)

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:       name,
		Pane:       pane,
		State:      state,
		WorkflowID: workflowID,
		Cwd:        t.TempDir(),
	}))

	got, err := st.Agents.GetInWorkflow(ctx, workflowID, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	require.NoError(t, st.Agents.SetBranchByID(ctx, got.ID, xptr.Ptr(branch)))

	got, err = st.Agents.GetInWorkflow(ctx, workflowID, name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

func metadataOf(t *testing.T, st *testStore, name string) []agentstore.Metadata {
	t.Helper()
	got, err := st.Get(context.Background(), name)
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.Metadata
}

// TestReconcileRecordsAPullRequestOnceIdle covers the point of checking at all: an agent
// that opened a PR by shelling out to gh directly, bypassing every weave tool that would
// otherwise have recorded it.
func TestReconcileRecordsAPullRequestOnceIdle(t *testing.T) {
	st := newStore(t)
	seedWithBranch(t, st, "agent", "%1", agentstore.StateThinking, "harry-fix")

	tmux := newFakeTmux() // pane not owned, so the agent settles idle in this one pass
	finder := &fakePRFinder{url: "https://github.com/harrisoncramer/weave/pull/1"}

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents).WithPRFinder(finder)
	require.NoError(t, rec.Reconcile(context.Background()))
	require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))

	require.Eventually(t, func() bool {
		return len(metadataOf(t, st, "agent")) == 1
	}, time.Second, 5*time.Millisecond, "the found pull request is recorded")

	metadata := metadataOf(t, st, "agent")
	assert.Equal(t, "pr", metadata[0].Key)
	assert.Equal(t, finder.url, metadata[0].Value)
}

// TestReconcileChecksOnceThenLeavesItAlone covers the other half: a pull request already
// recorded is never looked up again, an agent going idle repeatedly is not asked about it
// on every one of those passes, and an agent with no branch recorded is never asked at all.
func TestReconcileChecksOnceThenLeavesItAlone(t *testing.T) {
	t.Run("a pull request already recorded is not looked up again", func(t *testing.T) {
		st := newStore(t)
		agent := seedWithBranch(t, st, "agent", "%1", agentstore.StateThinking, "harry-fix")
		_, err := st.Agents.SetMetadata(context.Background(), agentstore.SetMetadataParams{
			ID: agent.ID, Key: "pr", Value: xptr.Ptr("https://github.com/harrisoncramer/weave/pull/1"),
		})
		require.NoError(t, err)

		tmux := newFakeTmux()
		finder := &fakePRFinder{url: "https://github.com/harrisoncramer/weave/pull/2"}

		rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents).WithPRFinder(finder)
		require.NoError(t, rec.Reconcile(context.Background()))
		require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))

		time.Sleep(50 * time.Millisecond) // give a wrongly-fired lookup a chance to land
		assert.Equal(t, 0, finder.calls, "an already-recorded pull request is not re-checked")
	})

	t.Run("an agent with no branch is never asked", func(t *testing.T) {
		st := newStore(t)
		seed(t, st, "agent", "%1", agentstore.StateThinking)

		tmux := newFakeTmux()
		finder := &fakePRFinder{url: "https://github.com/harrisoncramer/weave/pull/1"}

		rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents).WithPRFinder(finder)
		require.NoError(t, rec.Reconcile(context.Background()))
		require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))

		time.Sleep(50 * time.Millisecond)
		assert.Equal(t, 0, finder.calls, "an agent weave never recorded a branch for is not asked")
	})

	t.Run("settling on idle again does not ask twice", func(t *testing.T) {
		st := newStore(t)
		agent := seedWithBranch(t, st, "agent", "%1", agentstore.StateIdle, "harry-fix")
		backdateStateSeen(t, st, agent.ID, stateSeenCoalesce+time.Second)

		tmux := newFakeTmux() // still not owned: reconcile re-confirms idle, a heartbeat, not a transition
		finder := &fakePRFinder{url: "https://github.com/harrisoncramer/weave/pull/1"}

		rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents).WithPRFinder(finder)
		require.NoError(t, rec.Reconcile(context.Background()))

		time.Sleep(50 * time.Millisecond)
		assert.Equal(t, 0, finder.calls, "re-confirming idle is not the transition into it")
	})

	t.Run("no finder wired means nothing is ever asked", func(t *testing.T) {
		st := newStore(t)
		seedWithBranch(t, st, "agent", "%1", agentstore.StateThinking, "harry-fix")

		tmux := newFakeTmux()
		rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents)
		require.NoError(t, rec.Reconcile(context.Background()))
		require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))

		assert.Empty(t, metadataOf(t, st, "agent"))
	})
}

// TestReconcileSwallowsALookupFailure covers gh being unavailable or timing out: a check
// nobody asked for should not spam the log at a level anyone is paged on.
func TestReconcileSwallowsALookupFailure(t *testing.T) {
	st := newStore(t)
	seedWithBranch(t, st, "agent", "%1", agentstore.StateThinking, "harry-fix")

	tmux := newFakeTmux()
	finder := &fakePRFinder{err: errors.New("gh: command not found")}

	rec := NewReconciler(tmux, fakeHarnesses{harness: &fakeHarness{}}, st.Agents).WithPRFinder(finder)
	require.NoError(t, rec.Reconcile(context.Background()))

	require.Eventually(t, func() bool { return finder.calls == 1 }, time.Second, 5*time.Millisecond)
	assert.Empty(t, metadataOf(t, st, "agent"), "a failed lookup records nothing")
}
