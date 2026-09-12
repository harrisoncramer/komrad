package agents

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

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
	branches := fakeBranches{
		onMain:    xptr.Ptr("main"),
		onFeature: xptr.Ptr("feature/x"),
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

// fakeBranches is the branch git would read for each directory, and no branch for any other.
type fakeBranches map[string]*string

func (f fakeBranches) GetCurrentBranch(_ context.Context, dir string) *string {
	return f[dir]
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

	looked := &recordingBranches{}
	syncer := NewCwdSyncer(st.Agents, looked)

	require.NoError(t, syncer.Sync(ctx))

	assert.Empty(t, looked.dirs, "a cwd that is gone is never asked for its branch")

	got, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.True(t, got.WorktreeRemoved)
}

// recordingBranches is a git that answers nothing and remembers what it was asked about.
type recordingBranches struct {
	dirs []string
}

func (r *recordingBranches) GetCurrentBranch(_ context.Context, dir string) *string {
	r.dirs = append(r.dirs, dir)

	return nil
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

	syncer := NewCwdSyncer(st.Agents, fakeBranches{repo: xptr.Ptr("main")})
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

	syncer := NewCwdSyncer(st.Agents, fakeBranches{})
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

			require.NoError(t, NewCwdSyncer(st.Agents, fakeBranches{tt.cwd: xptr.Ptr("main")}).Sync(ctx))

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

func initRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init", "--quiet", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@weave.test")
	runGit(t, dir, "config", "user.name", "weave test")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644))
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "--quiet", "-m", "initial")

	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := cmd.CombinedOutput()
	require.NoErrorf(t, err, "git %v: %s", args, output)
}

func removedDir(t *testing.T) string {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(dir, 0o755))
	require.NoError(t, os.Remove(dir))

	return dir
}
