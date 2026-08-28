package agents

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	agentstore "weave/internal/store/agent"
	"weave/lib/xptr"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBranchSyncerSync covers what the periodic pass writes for a live agent: the branch a
// git checkout is on, cleared for one that names no directory, is not a checkout at all, or
// sits on a detached HEAD rather than a named branch.
func TestBranchSyncerSync(t *testing.T) {
	repoOnMain := initRepo(t)

	repoOnFeature := initRepo(t)
	runGit(t, repoOnFeature, "checkout", "--quiet", "-b", "feature/x")

	detached := initRepo(t)
	sha := strings.TrimSpace(gitOutput(t, detached, "rev-parse", "HEAD"))
	runGit(t, detached, "checkout", "--quiet", sha)

	tests := []struct {
		name       string
		cwd        string
		wantBranch *string
	}{
		{
			name:       "a blank cwd carries no branch",
			cwd:        "",
			wantBranch: nil,
		},
		{
			name:       "a directory that is not a git checkout carries no branch",
			cwd:        t.TempDir(),
			wantBranch: nil,
		},
		{
			name:       "a detached HEAD is not a named branch",
			cwd:        detached,
			wantBranch: nil,
		},
		{
			name:       "a checkout on main reads main",
			cwd:        repoOnMain,
			wantBranch: xptr.Ptr("main"),
		},
		{
			name:       "a checkout on another branch reads that branch",
			cwd:        repoOnFeature,
			wantBranch: xptr.Ptr("feature/x"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newStore(t)
			ctx := t.Context()

			workflowID := seedWorkflow(t, st)
			require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{Name: "a", WorkflowID: workflowID, Cwd: tt.cwd}))
			created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
			require.NoError(t, err)

			require.NoError(t, NewBranchSyncer(st.Agents).Sync(ctx))

			got, err := st.Agents.GetByID(ctx, created.ID)
			require.NoError(t, err)
			require.NotNil(t, got)
			if tt.wantBranch == nil {
				assert.Nil(t, got.Branch)
				return
			}

			require.NotNil(t, got.Branch)
			assert.Equal(t, *tt.wantBranch, *got.Branch)
		})
	}
}

// TestBranchSyncerSyncClearsAStaleBranch covers an agent whose cwd changed out from under
// it, from a checkout to something that is not one: the next pass has to clear what an
// earlier pass wrote rather than leave it stale.
func TestBranchSyncerSyncClearsAStaleBranch(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	repo := initRepo(t)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{Name: "a", WorkflowID: workflowID, Cwd: repo}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)

	syncer := NewBranchSyncer(st.Agents)
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

// TestBranchSyncerSyncSkipsArchivedAgents covers that the pass walks live agents only,
// since an archived one's cwd may no longer even exist and there is nothing left to show
// its branch beside.
func TestBranchSyncerSyncSkipsArchivedAgents(t *testing.T) {
	st := newStore(t)
	ctx := t.Context()

	workflowID := seedWorkflow(t, st)
	repo := initRepo(t)
	require.NoError(t, st.Agents.Create(ctx, agentstore.Agent{Name: "a", WorkflowID: workflowID, Cwd: repo}))
	created, err := st.Agents.GetInWorkflow(ctx, workflowID, "a")
	require.NoError(t, err)
	_, archiveErr := st.Agents.Archive(ctx, []string{created.ID})
	require.NoError(t, archiveErr)

	require.NoError(t, NewBranchSyncer(st.Agents).Sync(ctx))

	got, err := st.Agents.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Nil(t, got.Branch, "an archived agent is not walked, so nothing writes a branch for it")
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	require.NoError(t, err)

	return string(out)
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
