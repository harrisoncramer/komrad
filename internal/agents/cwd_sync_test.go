package agents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"weave/internal/git"
	agentstore "weave/internal/store/agent"
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
			st := newStore(t)
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

			require.NoError(t, NewCwdSyncer(st.Agents, branches).Sync(ctx))

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

// fakeHeads is what git would read for each directory, and nothing at all for any other.
type fakeHeads map[string]git.HeadState

func (f fakeHeads) ReadHeadState(_ context.Context, dir string) git.HeadState {
	return f[dir]
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
			st := newStore(t)
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
			require.NoError(t, st.Agents.SetRepositoryURLByID(ctx, created.ID, tt.before))

			require.NoError(t, NewCwdSyncer(st.Agents, tt.heads(cwd)).Sync(ctx))

			got, err := st.Agents.GetByID(ctx, created.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.RepositoryURL)
		})
	}
}

// TestCwdSyncerSyncLooksAtEveryAgent covers the fan-out: a pass over more agents than it has
// workers still reads every directory and writes every row.
func TestCwdSyncerSyncLooksAtEveryAgent(t *testing.T) {
	st := newStore(t)
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

	require.NoError(t, NewCwdSyncer(st.Agents, heads).Sync(ctx))

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
	st := newStore(t)
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
	require.NoError(t, NewCwdSyncer(st.Agents, stopped).Sync(ctx))

	got, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.True(t, got.SequenceInProgress)
	require.NotNil(t, got.Branch)
	assert.Equal(t, "topic", *got.Branch)

	// Settled by hand in the worktree, which nothing tells weave about.
	require.NoError(t, NewCwdSyncer(st.Agents, fakeHeads{repo: {Branch: "topic"}}).Sync(ctx))

	after, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.False(t, after.SequenceInProgress)
}

// TestCwdSyncerSyncSkipsTheGitCallForARemovedCwd covers the waste the single pass exists to
// avoid: a cwd that is gone has no branch to read, so nothing waits out a git call against it.
func TestCwdSyncerSyncSkipsTheGitCallForARemovedCwd(t *testing.T) {
	st := newStore(t)
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
	syncer := NewCwdSyncer(st.Agents, looked)

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

func (r *recordingHeads) ReadHeadState(_ context.Context, dir string) git.HeadState {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirs = append(r.dirs, dir)

	return git.HeadState{}
}

// TestCwdSyncerSyncClearsAStaleBranch covers an agent whose cwd changed out from under it,
// from a checkout to something that is not one: the next pass has to clear what an earlier
// pass wrote rather than leave it stale.
func TestCwdSyncerSyncClearsAStaleBranch(t *testing.T) {
	st := newStore(t)
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

	syncer := NewCwdSyncer(st.Agents, fakeHeads{repo: {Branch: "main"}})
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
	st := newStore(t)
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

	syncer := NewCwdSyncer(st.Agents, fakeHeads{})
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
			st := newStore(t)
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

			require.NoError(t, NewCwdSyncer(st.Agents, fakeHeads{tt.cwd: {Branch: "main"}}).Sync(ctx))

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
