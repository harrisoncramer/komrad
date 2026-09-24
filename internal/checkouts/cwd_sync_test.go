package checkouts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"
	reviewstore "weave/internal/store/review"
	workflowstore "weave/internal/store/workflow"
	"weave/internal/storetest"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCwdSyncerSync covers what the periodic pass writes for a live agent: whether its cwd
// still exists, and the branch git names for it, cleared for a cwd that names no directory or
// that git names no branch for.
func TestCwdSyncerSync(t *testing.T) {
	onMain := fakeCheckout(t)
	onFeature := fakeCheckout(t)

	// What git answers for each directory, which internal/git's own tests pin against a real
	// repository.
	branches := fakeHeads{
		onMain:    {Branch: "main"},
		onFeature: {Branch: "feature/x"},
	}

	tests := []struct {
		name        string
		cwd         string
		wantBranch  *string
		wantRemoved bool
	}{
		{
			name:        "a blank cwd carries no branch and is not reported removed",
			cwd:         "",
			wantBranch:  nil,
			wantRemoved: false,
		},
		{
			name:        "a directory that is not a git checkout carries no branch",
			cwd:         t.TempDir(),
			wantBranch:  nil,
			wantRemoved: false,
		},
		{
			name:        "a checkout on main reads main",
			cwd:         onMain,
			wantBranch:  xptr.Ptr("main"),
			wantRemoved: false,
		},
		{
			name:        "a checkout on another branch reads that branch",
			cwd:         onFeature,
			wantBranch:  xptr.Ptr("feature/x"),
			wantRemoved: false,
		},
		{
			name:        "a cwd that no longer exists is reported removed and carries no branch",
			cwd:         removedDir(t),
			wantBranch:  nil,
			wantRemoved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			ctx := t.Context()

			workflowID := seedWorkflow(t, st)
			require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
				Name:       "a",
				State:      agentstore.StateIdle,
				WorkflowID: workflowID,
				Cwd:        tt.cwd,
			}))
			created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
			require.NoError(t, err)

			require.NoError(t, newCwdSyncer(st.Agents, branches).Sync(ctx))

			got, err := st.Agents.GetByID(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantRemoved, got.WorktreeRemoved)
			if tt.wantBranch == nil {
				assert.Nil(t, got.Branch)
				return
			}

			require.NotNil(t, got.Branch)
			assert.Equal(t, *tt.wantBranch, *got.Branch)
		})
	}
}

// newCwdSyncer is a syncer over a git that has no rounds to compare against.
func newCwdSyncer(agents cwdSetter, heads headReader) *CwdSyncer {
	return NewCwdSyncer(NewCwdSyncerParams{
		Agents: agents,
		Heads:  heads,
		Trees:  fakeTrees{},
		Rounds: fakeTallies{},
		Bases:  fakeBases{},
	})
}

// fakeHeads is what git would read for each directory, and nothing at all for any other.
type fakeHeads map[string]git.HeadState

func (f fakeHeads) ReadHeadState(_ context.Context, params git.ReadHeadStateParams) git.HeadState {
	return f[params.Dir]
}

// TestCwdSyncerSyncRecordsTheRepository covers the identity the pull request join sits on: the
// canonical address git reads for a checkout is written, rewritten when the origin moves, and
// cleared when the cwd stops naming a forge repository or stops existing at all.
func TestCwdSyncerSyncRecordsTheRepository(t *testing.T) {
	const weave = "https://github.com/harrisoncramer/weave"
	const other = "https://gitlab.com/group/subgroup/repo"

	tests := []struct {
		name   string
		before *string
		cwd    func(t *testing.T) string
		heads  func(cwd string) fakeHeads
		want   *string
	}{
		{
			name:  "a checkout with a forge origin records its canonical address",
			cwd:   fakeCheckout,
			heads: func(cwd string) fakeHeads { return fakeHeads{cwd: {Branch: "main", RepositoryURL: weave}} },
			want:  xptr.Ptr(weave),
		},
		{
			name:   "an origin moved under a running agent is rewritten on the next pass",
			before: xptr.Ptr(weave),
			cwd:    fakeCheckout,
			heads:  func(cwd string) fakeHeads { return fakeHeads{cwd: {Branch: "main", RepositoryURL: other}} },
			want:   xptr.Ptr(other),
		},
		{
			name:   "a checkout whose origin is not a forge clears what an earlier pass wrote",
			before: xptr.Ptr(weave),
			cwd:    fakeCheckout,
			heads:  func(cwd string) fakeHeads { return fakeHeads{cwd: {Branch: "main"}} },
			want:   nil,
		},
		{
			name:   "a cwd that no longer exists clears the repository",
			before: xptr.Ptr(weave),
			cwd:    removedDir,
			heads:  func(cwd string) fakeHeads { return fakeHeads{cwd: {Branch: "main", RepositoryURL: weave}} },
			want:   nil,
		},
		{
			name:  "a directory that is not a checkout records no repository",
			cwd:   func(t *testing.T) string { return t.TempDir() },
			heads: func(string) fakeHeads { return fakeHeads{} },
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			ctx := t.Context()

			workflowID := seedWorkflow(t, st)
			cwd := tt.cwd(t)
			require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
				Name:       "a",
				State:      agentstore.StateIdle,
				WorkflowID: workflowID,
				Cwd:        cwd,
			}))
			created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
			require.NoError(t, err)
			require.NoError(t, st.Agents.SetCheckout(ctx, created.ID, agentstore.SetCheckoutParams{RepositoryURL: tt.before}))

			require.NoError(t, newCwdSyncer(st.Agents, tt.heads(cwd)).Sync(ctx))

			got, err := st.Agents.GetByID(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.RepositoryURL)
		})
	}
}

// TestCwdSyncerSyncRecordsTheHead covers the commit a finished pull request is measured
// against: written, rewritten when the checkout moves, and cleared when the cwd is gone.
func TestCwdSyncerSyncRecordsTheHead(t *testing.T) {
	tests := []struct {
		name   string
		before *string
		cwd    func(t *testing.T) string
		heads  func(cwd string) fakeHeads
		want   *string
	}{
		{
			name:  "a checkout records the commit it has checked out",
			cwd:   fakeCheckout,
			heads: func(cwd string) fakeHeads { return fakeHeads{cwd: {Branch: "main", SHA: "aaa"}} },
			want:  xptr.Ptr("aaa"),
		},
		{
			name:   "a commit made under a running agent is rewritten on the next pass",
			before: xptr.Ptr("aaa"),
			cwd:    fakeCheckout,
			heads:  func(cwd string) fakeHeads { return fakeHeads{cwd: {Branch: "main", SHA: "bbb"}} },
			want:   xptr.Ptr("bbb"),
		},
		{
			name:   "a cwd that no longer exists clears the commit",
			before: xptr.Ptr("aaa"),
			cwd:    removedDir,
			heads:  func(cwd string) fakeHeads { return fakeHeads{cwd: {Branch: "main", SHA: "aaa"}} },
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			ctx := t.Context()

			workflowID := seedWorkflow(t, st)
			cwd := tt.cwd(t)
			require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
				Name:       "a",
				State:      agentstore.StateIdle,
				WorkflowID: workflowID,
				Cwd:        cwd,
			}))
			created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
			require.NoError(t, err)
			require.NoError(t, st.Agents.SetCheckout(ctx, created.ID, agentstore.SetCheckoutParams{
				Branch:  xptr.Ptr("main"),
				HeadSHA: tt.before,
			}))

			require.NoError(t, newCwdSyncer(st.Agents, tt.heads(cwd)).Sync(ctx))

			got, err := st.Agents.GetByID(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.HeadSHA)
		})
	}
}

// TestCwdSyncerSyncLooksAtEveryAgent covers the fan-out: a pass over more agents than it has
// workers still reads every directory and writes every row.
func TestCwdSyncerSyncLooksAtEveryAgent(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	heads := fakeHeads{}
	count := cwdLookWorkers*2 + 1
	for i := range count {
		cwd := fakeCheckout(t)
		heads[cwd] = git.HeadState{Branch: fmt.Sprintf("b%d", i)}
		require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
			Name:       fmt.Sprintf("a%d", i),
			State:      agentstore.StateIdle,
			WorkflowID: workflowID,
			Cwd:        cwd,
		}))
	}

	require.NoError(t, newCwdSyncer(st.Agents, heads).Sync(ctx))

	agents, err := st.Agents.List(ctx)
	require.NoError(t, err)
	require.Len(t, agents, count)
	for _, agent := range agents {
		require.NotNil(t, agent.Branch, agent.Name)
		assert.Equal(t, heads[agent.Cwd].Branch, *agent.Branch)
	}
}

// TestCwdSyncerSyncRecordsAStoppedSequence covers the signal the row draws from: a checkout
// part-way through a rebase says so on the agent, and still names the branch the work is on.
func TestCwdSyncerSyncRecordsAStoppedSequence(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	repo := fakeCheckout(t)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
		Name:       "a",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Cwd:        repo,
	}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)

	stopped := fakeHeads{repo: {Branch: "topic", SequenceInProgress: true}}
	require.NoError(t, newCwdSyncer(st.Agents, stopped).Sync(ctx))

	got, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.True(t, got.SequenceInProgress)
	require.NotNil(t, got.Branch)
	assert.Equal(t, "topic", *got.Branch)

	// Settled by hand in the worktree, which nothing tells weave about.
	require.NoError(t, newCwdSyncer(st.Agents, fakeHeads{repo: {Branch: "topic"}}).Sync(ctx))

	after, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.False(t, after.SequenceInProgress)
}

// TestCwdSyncerSyncSkipsTheGitCallForARemovedCwd covers the waste the single pass exists to
// avoid: a cwd that is gone has no branch to read, so nothing waits out a git call against it.
func TestCwdSyncerSyncSkipsTheGitCallForARemovedCwd(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
		Name:       "a",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Cwd:        removedDir(t),
	}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)

	looked := &recordingHeads{}
	syncer := newCwdSyncer(st.Agents, looked)

	require.NoError(t, syncer.Sync(ctx))

	assert.Empty(t, looked.dirs, "a cwd that is gone is never asked for its branch")

	got, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.True(t, got.WorktreeRemoved)
}

// recordingHeads is a git that answers nothing and remembers what it was asked about.
type recordingHeads struct {
	mu   sync.Mutex
	dirs []string
}

func (r *recordingHeads) ReadHeadState(_ context.Context, params git.ReadHeadStateParams) git.HeadState {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirs = append(r.dirs, params.Dir)

	return git.HeadState{}
}

// TestCwdSyncerSyncClearsAStaleBranch covers an agent whose cwd changed out from under it,
// from a checkout to something that is not one: the next pass has to clear what an earlier
// pass wrote rather than leave it stale.
func TestCwdSyncerSyncClearsAStaleBranch(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	repo := fakeCheckout(t)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
		Name:       "a",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Cwd:        repo,
	}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)

	syncer := newCwdSyncer(st.Agents, fakeHeads{repo: {Branch: "main"}})
	require.NoError(t, syncer.Sync(ctx))

	got, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got.Branch)
	assert.Equal(t, "main", *got.Branch)

	got.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, *got))

	require.NoError(t, syncer.Sync(ctx))

	after, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Nil(t, after.Branch)
}

// TestCwdSyncerSyncClearsAStaleRemoval covers an agent whose cwd was recreated after
// vanishing: the next pass has to clear what an earlier pass wrote rather than leave it
// stuck as removed.
func TestCwdSyncerSyncClearsAStaleRemoval(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
		Name:       "a",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Cwd:        removedDir(t),
	}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)

	syncer := newCwdSyncer(st.Agents, fakeHeads{})
	require.NoError(t, syncer.Sync(ctx))

	got, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.True(t, got.WorktreeRemoved)

	got.Cwd = t.TempDir()
	require.NoError(t, st.Agents.Update(ctx, *got))

	require.NoError(t, syncer.Sync(ctx))

	after, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.False(t, after.WorktreeRemoved)
}

// TestCwdSyncerSyncSkipsArchivedAgents covers that the pass walks live agents only.
func TestCwdSyncerSyncSkipsArchivedAgents(t *testing.T) {
	tests := []struct {
		name string
		cwd  string
	}{
		{
			name: "a checkout is not read for its branch",
			cwd:  fakeCheckout(t),
		},
		{
			name: "a cwd that is gone is not reported removed",
			cwd:  removedDir(t),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := storetest.Open(t)
			ctx := t.Context()

			workflowID := seedWorkflow(t, st)
			require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
				Name:       "a",
				State:      agentstore.StateIdle,
				WorkflowID: workflowID,
				Cwd:        tt.cwd,
			}))
			created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
			require.NoError(t, err)
			_, archiveErr := st.Agents.Archive(ctx, []string{created.ID})
			require.NoError(t, archiveErr)

			require.NoError(t, newCwdSyncer(st.Agents, fakeHeads{tt.cwd: {Branch: "main"}}).Sync(ctx))

			got, err := st.Agents.GetByID(ctx, created.ID)
			require.NoError(t, err)
			assert.Nil(t, got.Branch)
			assert.False(t, got.WorktreeRemoved)
		})
	}
}

// fakeCheckout is a directory that reads as a primary checkout.
func fakeCheckout(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o755))

	return dir
}

func removedDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.Remove(dir))

	return dir
}

// fakeTrees is what git would answer about each directory against the one round it is asked
// about, and an error for any other.
type fakeTrees map[string]git.Standing

func (fakeTrees) ProbeTree(context.Context, string) (string, error) {
	return "", errors.New("no worktree to probe")
}

func (f fakeTrees) ReadStanding(_ context.Context, params git.ReadStandingParams) (git.Standing, error) {
	standing, ok := f[params.Dir]
	if !ok {
		return git.StandingSame, errors.New("no round to compare against")
	}

	return standing, nil
}

// standingOn is the standing the row records against the given round, and unmoved against any
// other.
func standingOn(t *testing.T, st *storetest.Stores, id, round string) git.Standing {
	t.Helper()

	agent, err := st.Agents.GetByID(t.Context(), id)
	require.NoError(t, err)
	require.NotNil(t, agent)
	if agent.TreeStandingSHA != round {
		return git.StandingSame
	}

	return agent.TreeStanding
}

// countingAgents is the agent store with its standing writes counted.
type countingAgents struct {
	cwdSetter
	standingWrites int
}

func (c *countingAgents) SetTreeStanding(ctx context.Context, id string, standing git.Standing, snapshotSHA string) error {
	c.standingWrites++

	return c.cwdSetter.SetTreeStanding(ctx, id, standing, snapshotSHA)
}

// TestCwdSyncerSyncWritesAStandingOnlyWhenItMoved pins that a pass writes the standing it
// found only where the row says otherwise, so a pass that finds every tree where the last one
// did writes nothing.
func TestCwdSyncerSyncWritesAStandingOnlyWhenItMoved(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	cwd := fakeCheckout(t)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
		Name:       "a",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Cwd:        cwd,
	}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)

	approved := reviewstore.VerdictApproved
	trees := fakeTrees{cwd: git.StandingSame}
	agents := &countingAgents{cwdSetter: st.Agents}
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents: agents,
		Heads:  fakeHeads{},
		Trees:  trees,
		Rounds: fakeTallies{{
			AgentID:     created.ID,
			Verdict:     &approved,
			SnapshotSHA: "snapshot-one",
		}},
		Bases: fakeBases{},
	})

	require.NoError(t, syncer.Sync(ctx))
	assert.Equal(t, 1, agents.standingWrites, "the first pass records the round the tree was measured against")
	assert.Equal(t, "snapshot-one", func() string {
		agent, err := st.Agents.GetByID(ctx, created.ID)
		require.NoError(t, err)

		return agent.TreeStandingSHA
	}())

	require.NoError(t, syncer.Sync(ctx))
	assert.Equal(t, 1, agents.standingWrites, "a pass that finds the tree where it was writes nothing")

	trees[cwd] = git.StandingMoved
	require.NoError(t, syncer.Sync(ctx))
	assert.Equal(t, 2, agents.standingWrites, "a tree that moved against its round is written")
	assert.Equal(t, git.StandingMoved, standingOn(t, st, created.ID, "snapshot-one"))
}

// failingTrees is a git that knows the tree moved but could not finish the landed question.
type failingTrees struct{}

func (failingTrees) ProbeTree(context.Context, string) (string, error) {
	return "", errors.New("no worktree to probe")
}

func (failingTrees) ReadStanding(context.Context, git.ReadStandingParams) (git.Standing, error) {
	return git.StandingMoved, errors.New("the base could not be merged")
}

type fakeBases []workflowstore.Workflow

func (f fakeBases) List(context.Context) ([]workflowstore.Workflow, error) {
	return f, nil
}

type fakeTallies []reviewstore.Tally

func (f fakeTallies) ListTallies(context.Context) ([]reviewstore.Tally, error) {
	return f, nil
}

func (fakeTallies) ListNewestRounds(context.Context) ([]reviewstore.NewestRound, error) {
	return nil, nil
}

// recordingTrees is a git that remembers which directories it was asked to hash, and against
// which base.
type recordingTrees struct {
	mu    sync.Mutex
	dirs  []string
	bases []string
}

func (r *recordingTrees) ProbeTree(context.Context, string) (string, error) {
	return "", errors.New("no worktree to probe")
}

func (r *recordingTrees) ReadStanding(_ context.Context, params git.ReadStandingParams) (git.Standing, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirs = append(r.dirs, params.Dir)
	r.bases = append(r.bases, params.Base)

	return git.StandingMoved, nil
}

// TestCwdSyncerSyncReadsWhereTheTreeStands pins that a worktree is reported as git finds it
// against the round of its last verdict, and that nothing is known of any other round.
func TestCwdSyncerSyncReadsWhereTheTreeStands(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	dirs := map[string]string{}
	for _, name := range []string{"edited", "untouched", "landed", "landed-edited"} {
		dirs[name] = fakeCheckout(t)
		require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
			Name:       name,
			State:      agentstore.StateIdle,
			WorkflowID: workflowID,
			Cwd:        dirs[name],
		}))
	}
	ids := map[string]string{}
	for name := range dirs {
		agent, err := st.Agents.GetInWorkflow(ctx, workflowID, name)
		require.NoError(t, err)
		ids[name] = agent.ID
	}

	approved := reviewstore.VerdictApproved
	tallies := fakeTallies{}
	for name, id := range ids {
		tallies = append(tallies, reviewstore.Tally{
			AgentID:     id,
			Verdict:     &approved,
			SnapshotSHA: "snapshot-" + name,
		})
	}
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents: st.Agents,
		Heads:  fakeHeads{},
		Trees: fakeTrees{
			dirs["edited"]:        git.StandingMoved,
			dirs["untouched"]:     git.StandingSame,
			dirs["landed"]:        git.StandingLanded,
			dirs["landed-edited"]: git.StandingLandedMoved,
		},
		Rounds: tallies,
		Bases:  fakeBases{},
	})

	assert.Equal(t, git.StandingSame, standingOn(t, st, ids["edited"], "snapshot-edited"), "nothing is recorded before the first pass")

	require.NoError(t, syncer.Sync(ctx))

	assert.Equal(t, git.StandingMoved, standingOn(t, st, ids["edited"], "snapshot-edited"))
	assert.Equal(t, git.StandingSame, standingOn(t, st, ids["untouched"], "snapshot-untouched"))
	assert.Equal(t, git.StandingLanded, standingOn(t, st, ids["landed"], "snapshot-landed"))
	assert.Equal(t, git.StandingLandedMoved, standingOn(t, st, ids["landed-edited"], "snapshot-landed-edited"))
	assert.Equal(t, git.StandingSame, standingOn(t, st, ids["edited"], "snapshot-newer"),
		"a verdict on a round the pass has not compared against is not one the tree moved from")
}

// TestCwdSyncerSyncKeepsAStandingReportedWithAnError pins that a failure after the tree was
// found to have moved is said, and never demotes the row to unmoved.
func TestCwdSyncerSyncKeepsAStandingReportedWithAnError(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
		Name:       "a",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Cwd:        fakeCheckout(t),
	}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)

	approved := reviewstore.VerdictApproved
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents: st.Agents,
		Heads:  fakeHeads{},
		Trees:  failingTrees{},
		Bases:  fakeBases{},
		Rounds: fakeTallies{{
			AgentID:     created.ID,
			Verdict:     &approved,
			SnapshotSHA: "snapshot-one",
		}},
	})

	err = syncer.Sync(ctx)
	require.Error(t, err)
	assert.Equal(t, git.StandingMoved, standingOn(t, st, created.ID, "snapshot-one"))
}

// TestCwdSyncerSyncMeasuresAgainstTheWorkflowsBase pins that the landed question is asked of
// the branch the workflow cuts from.
func TestCwdSyncerSyncMeasuresAgainstTheWorkflowsBase(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
		Name:       "reviewed",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Cwd:        fakeCheckout(t),
	}))
	agent, err := st.Agents.GetInWorkflow(ctx, workflowID, "reviewed")
	require.NoError(t, err)

	approved := reviewstore.VerdictApproved
	hashed := &recordingTrees{}
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents: st.Agents,
		Heads:  fakeHeads{},
		Trees:  hashed,
		Rounds: fakeTallies{{
			AgentID:     agent.ID,
			Verdict:     &approved,
			SnapshotSHA: "snapshot-one",
		}},
		Bases: fakeBases{{
			ID:       workflowID,
			Worktree: workflowstore.WorktreeConfig{BaseBranch: "develop"},
		}},
	})

	require.NoError(t, syncer.Sync(ctx))

	assert.Equal(t, []string{"develop"}, hashed.bases)
}

// TestCwdSyncerSyncHashesOnlyAReviewedTree covers the cost the pass keeps down: a worktree with
// no verdict on it, or none left on disk, is never hashed.
func TestCwdSyncerSyncHashesOnlyAReviewedTree(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	reviewed, unreviewed, commented := fakeCheckout(t), fakeCheckout(t), fakeCheckout(t)
	for name, cwd := range map[string]string{
		"reviewed":   reviewed,
		"unreviewed": unreviewed,
		"commented":  commented,
		"removed":    removedDir(t),
	} {
		require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
			Name:       name,
			State:      agentstore.StateIdle,
			WorkflowID: workflowID,
			Cwd:        cwd,
		}))
	}
	reviewedAgent, err := st.Agents.GetInWorkflow(ctx, workflowID, "reviewed")
	require.NoError(t, err)
	commentedAgent, err := st.Agents.GetInWorkflow(ctx, workflowID, "commented")
	require.NoError(t, err)
	removedAgent, err := st.Agents.GetInWorkflow(ctx, workflowID, "removed")
	require.NoError(t, err)

	approved := reviewstore.VerdictApproved
	hashed := &recordingTrees{}
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents: st.Agents,
		Heads:  fakeHeads{},
		Trees:  hashed,
		Bases:  fakeBases{},
		Rounds: fakeTallies{
			{
				AgentID:     reviewedAgent.ID,
				Verdict:     &approved,
				SnapshotSHA: "snapshot-one",
			},
			{AgentID: commentedAgent.ID, Rounds: 1},
			{
				AgentID:     removedAgent.ID,
				Verdict:     &approved,
				SnapshotSHA: "snapshot-three",
			},
		},
	})

	require.NoError(t, syncer.Sync(ctx))

	assert.Equal(t, []string{reviewed}, hashed.dirs)
	assert.Equal(t, git.StandingMoved, standingOn(t, st, reviewedAgent.ID, "snapshot-one"))
	assert.Equal(t, git.StandingSame, standingOn(t, st, removedAgent.ID, "snapshot-three"))
}

// TestCwdSyncerSyncForgetsAMoveTheNextPassDoesNotSee pins that each pass replaces the last:
// a round opened on the moved tree leaves nothing for the column to keep saying.
func TestCwdSyncerSyncForgetsAMoveTheNextPassDoesNotSee(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	cwd := fakeCheckout(t)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
		Name:       "a",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Cwd:        cwd,
	}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)

	approved := reviewstore.VerdictApproved
	trees := fakeTrees{cwd: git.StandingMoved}
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents: st.Agents,
		Heads:  fakeHeads{},
		Trees:  trees,
		Bases:  fakeBases{},
		Rounds: fakeTallies{{
			AgentID:     created.ID,
			Verdict:     &approved,
			SnapshotSHA: "snapshot-one",
		}},
	})

	require.NoError(t, syncer.Sync(ctx))
	require.Equal(t, git.StandingMoved, standingOn(t, st, created.ID, "snapshot-one"))

	trees[cwd] = git.StandingSame
	require.NoError(t, syncer.Sync(ctx))
	assert.Equal(t, git.StandingSame, standingOn(t, st, created.ID, "snapshot-one"))
}

// TestCwdSyncerSyncReportsAProbeItCouldNotMake pins that a tree the pass cannot hash is said
// rather than read as unmoved.
func TestCwdSyncerSyncReportsAProbeItCouldNotMake(t *testing.T) {
	st := storetest.Open(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	cwd := fakeCheckout(t)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{
		Name:       "a",
		State:      agentstore.StateIdle,
		WorkflowID: workflowID,
		Cwd:        cwd,
	}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)

	approved := reviewstore.VerdictApproved
	syncer := NewCwdSyncer(NewCwdSyncerParams{
		Agents: st.Agents,
		Heads:  fakeHeads{},
		Trees:  fakeTrees{},
		Bases:  fakeBases{},
		Rounds: fakeTallies{{
			AgentID:     created.ID,
			Verdict:     &approved,
			SnapshotSHA: "snapshot-one",
		}},
	})

	err = syncer.Sync(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `failed to compare the tree of "a"`)
	assert.Equal(t, git.StandingSame, standingOn(t, st, created.ID, "snapshot-one"))
}
