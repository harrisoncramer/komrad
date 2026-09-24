package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/gittest"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"
	worktreestore "weave/internal/store/worktree"
	"weave/internal/storetest"

	"github.com/stretchr/testify/require"
)

// fixture is a repository's .git directory with a worktrees directory beside it, laid out by
// hand the way git lays one out, over a real store.
type fixture struct {
	repo       string
	shared     string
	stores     *storetest.Stores
	worktrees  *worktreestore.WorktreeStore
	workflowID string
	git        *gittest.Checkout
	claims     *fakeClaims
	lister     *Lister
	now        time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	stores := storetest.Open(t)
	f := &fixture{
		repo:       filepath.Join(root, "repo"),
		shared:     filepath.Join(root, "trees"),
		stores:     stores,
		worktrees:  worktreestore.NewWorktreeStore(stores.Pool.GetDB()),
		workflowID: stores.SeedWorkflow(t, workflowstore.Config{Name: "flow"}).ID,
		git:        &gittest.Checkout{},
		claims:     &fakeClaims{},
		now:        time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, os.MkdirAll(filepath.Join(f.repo, ".git", "worktrees"), 0o755))
	require.NoError(t, os.MkdirAll(f.shared, 0o755))
	f.lister = f.listerOver(f.shared)

	return f
}

func (f *fixture) listerOver(shared string) *Lister {
	return NewLister(NewListerParams{
		SharedDir: func() (string, error) { return shared, nil },
		Agents:    f.stores.Agents,
		Workflows: f.stores.Workflows,
		Sizes:     f.worktrees,
	})
}

// cut lays out a worktree named with a fresh agent id: the directory with its .git file, and
// the gitdir in the repository with HEAD on the agent's branch.
func (f *fixture) cut(t *testing.T) string {
	t.Helper()

	id, err := store.NewID()
	require.NoError(t, err)

	dir := filepath.Join(f.shared, id)
	gitdir := filepath.Join(f.repo, ".git", "worktrees", id)
	write(t, dir, ".git", "gitdir: "+gitdir+"\n")
	write(t, gitdir, "HEAD", "ref: refs/heads/agent/"+id+"\n")
	write(t, dir, "own.go", "package main\n")

	return dir
}

// own records the agent a directory the fixture cut was made for.
func (f *fixture) own(t *testing.T, dir string, agent agentstore.Agent) *agentstore.Agent {
	t.Helper()

	agent.ID = filepath.Base(dir)
	agent.Cwd = dir
	if agent.WorkflowID == "" {
		agent.WorkflowID = f.workflowID
	}

	return f.stores.SeedAgent(t, agent)
}

// occupy records an agent working in a directory it was not made for.
func (f *fixture) occupy(t *testing.T, cwd, name string) {
	t.Helper()

	f.stores.SeedAgent(t, agentstore.Agent{
		Name:       name,
		Cwd:        cwd,
		WorkflowID: f.workflowID,
	})
}

func (f *fixture) archive(t *testing.T, id string) {
	t.Helper()

	_, err := f.stores.Agents.Archive(t.Context(), []string{id})
	require.NoError(t, err)
}

// measured stores a directory's row with a measurement taken at the time given.
func (f *fixture) measured(t *testing.T, dir string, at time.Time) {
	t.Helper()

	require.NoError(t, f.worktrees.Upsert(t.Context(), worktreestore.Worktree{Dir: dir}))
	require.NoError(t, f.worktrees.RecordSize(t.Context(), worktreestore.Size{
		Dir:        dir,
		Bytes:      1,
		MeasuredAt: at,
	}))
}

// stored reads every row the store holds, keyed by directory.
func (f *fixture) stored(t *testing.T) map[string]worktreestore.Worktree {
	t.Helper()

	listed, err := f.worktrees.List(t.Context())
	require.NoError(t, err)

	byDir := make(map[string]worktreestore.Worktree, len(listed))
	for _, worktree := range listed {
		byDir[worktree.Dir] = worktree
	}

	return byDir
}

func (f *fixture) inspector() *Inspector {
	return NewInspector(NewInspectorParams{
		Lister:    f.lister,
		Git:       f.git,
		Workflows: f.stores.Workflows,
		Sizes:     f.worktrees,
		Now:       func() time.Time { return f.now },
	})
}

func (f *fixture) measurer() *Measurer {
	return NewMeasurer(NewMeasurerParams{
		Lister:    f.lister,
		Claimed:   f.claims,
		Worktrees: f.worktrees,
		Now:       func() time.Time { return f.now },
	})
}

func (f *fixture) remover() *Remover {
	return NewRemover(NewRemoverParams{
		Lister: f.lister,
		Agents: f.stores.Agents,
		Git:    f.git,
	})
}

// fakeClaims is the directories an unfinished delete names, which the job queue holds.
type fakeClaims struct {
	dirs []string
}

func (c *fakeClaims) ListClaimedDirs(context.Context) ([]string, error) {
	return c.dirs, nil
}

func write(t *testing.T, root, path, content string) {
	t.Helper()

	full := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}
