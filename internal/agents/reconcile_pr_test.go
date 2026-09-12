package agents

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
	"weave/internal/harness/pane"

	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePRFinder is read from the test goroutine and written from the goroutine checkPR
// spawns, so its call count is atomic: nothing else here crosses goroutines.
type fakePRFinder struct {
	url   string
	err   error
	calls atomic.Int64
}

func (f *fakePRFinder) FindPR(context.Context, string, string) (string, error) {
	f.calls.Add(1)

	return f.url, f.err
}

func seedWithBranch(t *testing.T, st *testStore, state agentstore.State) agentstore.Agent {
	t.Helper()
	ctx := t.Context()
	workflowID := seedWorkflow(t, st)

	require.NoError(t, st.Create(ctx, agentstore.Agent{
		Name:       "agent",
		Pane:       "%1",
		State:      state,
		WorkflowID: workflowID,
		Cwd:        t.TempDir(),
	}))

	got, err := st.Agents.GetInWorkflow(ctx, workflowID, "agent")
	require.NoError(t, err)
	require.NotNil(t, got)

	require.NoError(t, st.Agents.SetBranchByID(ctx, got.ID, xptr.Ptr("harry-fix")))

	got, err = st.Agents.GetInWorkflow(ctx, workflowID, "agent")
	require.NoError(t, err)
	require.NotNil(t, got)

	return *got
}

func metadataOf(t *testing.T, st *testStore) []agentstore.Metadata {
	t.Helper()
	got, err := st.Get(t.Context(), "agent")
	require.NoError(t, err)
	require.NotNil(t, got)

	return got.Metadata
}

// TestReconcileRecordsAPullRequestOnceIdle covers the point of checking at all: an agent
// that opened a PR by shelling out to gh directly, bypassing every weave tool that would
// otherwise have recorded it.
func TestReconcileRecordsAPullRequestOnceIdle(t *testing.T) {
	st := newStore(t)
	seedWithBranch(t, st, agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = "" // a pane that is not the agent's, so it settles idle in this one pass
	finder := &fakePRFinder{url: "https://github.com/harrisoncramer/weave/pull/1"}

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	}).WithPRFinder(finder)
	require.NoError(t, rec.Reconcile(t.Context()))
	require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))

	require.Eventually(t, func() bool {
		return len(metadataOf(t, st)) == 1
	}, time.Second, 5*time.Millisecond, "the found pull request is recorded")

	metadata := metadataOf(t, st)
	assert.Equal(t, "pr", metadata[0].Key)
	assert.Equal(t, finder.url, metadata[0].Value)
}

// TestReconcileChecksOnceThenLeavesItAlone covers the other half: a pull request already
// recorded is never looked up again, an agent going idle repeatedly is not asked about it
// on every one of those passes, and an agent with no branch recorded is never asked at all.
func TestReconcileChecksOnceThenLeavesItAlone(t *testing.T) {
	t.Run("a pull request already recorded is not looked up again", func(t *testing.T) {
		st := newStore(t)
		agent := seedWithBranch(t, st, agentstore.StateThinking)
		_, err := st.Agents.SetMetadata(t.Context(), agentstore.SetMetadataParams{
			ID: agent.ID, Key: "pr", Value: xptr.Ptr("https://github.com/harrisoncramer/weave/pull/1"),
		})
		require.NoError(t, err)

		tmux := newFakeTmux()
		tmux.owners["%1"] = ""
		finder := &fakePRFinder{url: "https://github.com/harrisoncramer/weave/pull/2"}

		rec := NewReconciler(NewReconcilerParams{
			Panes:    tmux,
			Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
			States:   st.Agents,
		}).WithPRFinder(finder)
		require.NoError(t, rec.Reconcile(t.Context()))
		require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))

		time.Sleep(50 * time.Millisecond) // give a wrongly-fired lookup a chance to land
		assert.Equal(t, int64(0), finder.calls.Load(), "an already-recorded pull request is not re-checked")
	})

	t.Run("an agent with no branch is never asked", func(t *testing.T) {
		st := newStore(t)
		seed(t, st, "agent", "%1", agentstore.StateThinking)

		tmux := newFakeTmux()
		tmux.owners["%1"] = ""
		finder := &fakePRFinder{url: "https://github.com/harrisoncramer/weave/pull/1"}

		rec := NewReconciler(NewReconcilerParams{
			Panes:    tmux,
			Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
			States:   st.Agents,
		}).WithPRFinder(finder)
		require.NoError(t, rec.Reconcile(t.Context()))
		require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))

		time.Sleep(50 * time.Millisecond)
		assert.Equal(t, int64(0), finder.calls.Load(), "an agent weave never recorded a branch for is not asked")
	})

	t.Run("settling on idle again does not ask twice", func(t *testing.T) {
		st := newStore(t)
		agent := seedWithBranch(t, st, agentstore.StateIdle)
		backdateStateSeen(t, st, agent.ID, stateSeenCoalesce+time.Second)

		tmux := newFakeTmux() // still not owned: reconcile re-confirms idle, a heartbeat, not a transition
		finder := &fakePRFinder{url: "https://github.com/harrisoncramer/weave/pull/1"}

		rec := NewReconciler(NewReconcilerParams{
			Panes:    tmux,
			Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
			States:   st.Agents,
		}).WithPRFinder(finder)
		require.NoError(t, rec.Reconcile(t.Context()))

		time.Sleep(50 * time.Millisecond)
		assert.Equal(t, int64(0), finder.calls.Load(), "re-confirming idle is not the transition into it")
	})

	t.Run("no finder wired means nothing is ever asked", func(t *testing.T) {
		st := newStore(t)
		seedWithBranch(t, st, agentstore.StateThinking)

		tmux := newFakeTmux()
		tmux.owners["%1"] = ""
		rec := NewReconciler(NewReconcilerParams{
			Panes:    tmux,
			Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
			States:   st.Agents,
		})
		require.NoError(t, rec.Reconcile(t.Context()))
		require.Equal(t, agentstore.StateIdle, stateOf(t, st, "agent"))

		assert.Empty(t, metadataOf(t, st))
	})
}

// TestReconcileSwallowsALookupFailure covers gh being unavailable or timing out: a check
// nobody asked for should not spam the log at a level anyone is paged on.
func TestReconcileSwallowsALookupFailure(t *testing.T) {
	st := newStore(t)
	seedWithBranch(t, st, agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = ""
	finder := &fakePRFinder{err: errors.New("gh: command not found")}

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	}).WithPRFinder(finder)
	require.NoError(t, rec.Reconcile(t.Context()))

	require.Eventually(t, func() bool { return finder.calls.Load() == 1 }, time.Second, 5*time.Millisecond)
	assert.Empty(t, metadataOf(t, st), "a failed lookup records nothing")
}

// TestReconcileBacksOffAfterARateLimitedLookup covers what a real GitHub rate limit does:
// this only ever runs once per idle transition, so the failure mode to guard is an agent
// that cycles through work and idle often, hitting gh again on every cycle rather than
// waiting the failure out.
func TestReconcileBacksOffAfterARateLimitedLookup(t *testing.T) {
	st := newStore(t)
	agent := seedWithBranch(t, st, agentstore.StateThinking)

	tmux := newFakeTmux()
	tmux.owners["%1"] = ""
	finder := &fakePRFinder{err: errors.New("API rate limit exceeded for installation ID 123.")}

	rec := NewReconciler(NewReconcilerParams{
		Panes:    tmux,
		Observer: pane.New(pane.Params{Terminal: tmux, Harnesses: fakeHarnesses{harness: &fakeHarness{}}}),
		States:   st.Agents,
	}).WithPRFinder(finder)
	at := time.Now()
	rec.tally.now = func() time.Time { return at }

	require.NoError(t, rec.Reconcile(t.Context()))
	require.Eventually(t, func() bool { return finder.calls.Load() == 1 }, time.Second, 5*time.Millisecond)

	require.NoError(t, st.Agents.SetStateByID(t.Context(), agent.ID, agentstore.StateThinking))
	require.NoError(t, rec.Reconcile(t.Context()))
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int64(1), finder.calls.Load(), "still inside the backoff window, so not asked again")

	at = at.Add(prRateLimitBackoff + time.Second)
	require.NoError(t, st.Agents.SetStateByID(t.Context(), agent.ID, agentstore.StateThinking))
	require.NoError(t, rec.Reconcile(t.Context()))
	require.Eventually(t, func() bool { return finder.calls.Load() == 2 }, time.Second, 5*time.Millisecond,
		"the backoff window has passed")
}
