package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"weave/internal/git"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	workflowstore "weave/internal/store/workflow"

	"github.com/stretchr/testify/require"
)

// fixture is a repository's .git directory with a worktrees directory beside it, laid out by
// hand the way git lays one out, and the fakes a lister reads.
type fixture struct {
	repo   string
	shared string
	owners *fakeOwners
	flows  *fakeWorkflows
	git    *fakeGit
	lister *Lister
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	f := &fixture{
		repo:   filepath.Join(root, "repo"),
		shared: filepath.Join(root, "trees"),
		owners: &fakeOwners{},
		flows:  &fakeWorkflows{flows: map[string]*workflowstore.Workflow{}},
		git:    &fakeGit{},
	}
	require.NoError(t, os.MkdirAll(filepath.Join(f.repo, ".git", "worktrees"), 0o755))
	require.NoError(t, os.MkdirAll(f.shared, 0o755))
	f.lister = NewLister(NewListerParams{
		SharedDir: func() (string, error) { return f.shared, nil },
		Agents:    f.owners,
		Workflows: f.flows,
	})

	return f
}

// cut lays out a worktree named after an agent id: the directory with its .git file, and the
// gitdir in the repository with HEAD on the agent's branch.
func (f *fixture) cut(t *testing.T, id string) string {
	t.Helper()

	dir := filepath.Join(f.shared, id)
	gitdir := filepath.Join(f.repo, ".git", "worktrees", id)
	write(t, dir, ".git", "gitdir: "+gitdir+"\n")
	write(t, gitdir, "HEAD", "ref: refs/heads/agent/"+id+"\n")
	write(t, dir, "own.go", "package main\n")

	return dir
}

// own records an agent row for a directory the fixture cut.
func (f *fixture) own(id, name string, archivedAt *time.Time) {
	f.owners.rows = append(f.owners.rows, agentstore.AgentDir{
		ID:         id,
		Name:       name,
		Cwd:        filepath.Join(f.shared, id),
		State:      agentstore.StateIdle,
		ArchivedAt: archivedAt,
		WorkflowID: "flow",
	})
}

func (f *fixture) inspector() *Inspector {
	return NewInspector(NewInspectorParams{Lister: f.lister, Git: f.git, Workflows: f.flows})
}

func (f *fixture) remover() *Remover {
	return NewRemover(NewRemoverParams{Lister: f.lister, Git: f.git})
}

type fakeOwners struct {
	rows []agentstore.AgentDir
}

func (o *fakeOwners) ListDirs(context.Context) ([]agentstore.AgentDir, error) {
	return o.rows, nil
}

type fakeWorkflows struct {
	dirs  []string
	flows map[string]*workflowstore.Workflow
}

func (w *fakeWorkflows) ListWorktreeDirs(context.Context) ([]string, error) {
	return w.dirs, nil
}

func (w *fakeWorkflows) GetIncludingArchived(_ context.Context, id string) (*workflowstore.Workflow, error) {
	if flow, ok := w.flows[id]; ok {
		return flow, nil
	}

	return nil, store.ErrNotFound
}

// fakeGit answers every question about a checkout with what the test put in it, remembering
// which base it was asked to resolve and which repositories it was asked to prune.
type fakeGit struct {
	changes    git.Changes
	state      git.TreeState
	ahead      int
	behind     int
	prediction git.Prediction
	predictErr error
	held       bool
	unresolved bool
	commits    []git.Commit
	resolved   string
	resolves   int
	fetched    string
	fetches    int
	pruned     []string
	registered []git.Worktree
	listErr    error
}

func (g *fakeGit) ReadChanges(context.Context, string) (git.Changes, error) {
	return g.changes, nil
}

func (g *fakeGit) ReadTreeState(context.Context, string) (git.TreeState, error) {
	return g.state, nil
}

func (g *fakeGit) ResolveBase(_ context.Context, _, base string) (string, bool) {
	g.resolved = base
	g.resolves++
	if g.unresolved {
		return "HEAD", false
	}
	if base == "" {
		return "origin/HEAD", true
	}

	return "origin/" + base, true
}

func (g *fakeGit) FetchBase(_ context.Context, _, base string) (string, string, error) {
	g.fetched = base
	g.fetches++

	return "origin/" + base, "abc", nil
}

func (g *fakeGit) CountAheadBehind(context.Context, string, string) (int, int, error) {
	return g.ahead, g.behind, nil
}

func (g *fakeGit) PredictRebase(context.Context, string, string) (git.Prediction, error) {
	return g.prediction, g.predictErr
}

func (g *fakeGit) IsBranchHeldElsewhere(context.Context, string, string) (bool, error) {
	return g.held, nil
}

func (g *fakeGit) ListCommitsSince(context.Context, string, string, int) ([]git.Commit, error) {
	return g.commits, nil
}

func (g *fakeGit) ListWorktrees(context.Context, string) ([]git.Worktree, error) {
	return g.registered, g.listErr
}

func (g *fakeGit) PruneWorktrees(_ context.Context, repo string) error {
	g.pruned = append(g.pruned, repo)

	return nil
}

func write(t *testing.T, root, path, content string) {
	t.Helper()

	full := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}
