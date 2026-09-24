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

	"github.com/stretchr/testify/require"
)

// fixture is a repository's .git directory with a worktrees directory beside it, laid out by
// hand the way git lays one out, and the fakes a lister reads.
type fixture struct {
	repo   string
	shared string
	owners *fakeOwners
	flows  *fakeWorkflows
	git    *gittest.Checkout
	sizes  *fakeWorktreeStore
	claims *fakeClaims
	lister *Lister
	now    time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	f := &fixture{
		repo:   filepath.Join(root, "repo"),
		shared: filepath.Join(root, "trees"),
		owners: &fakeOwners{},
		flows: &fakeWorkflows{flows: map[string]*workflowstore.Workflow{
			"flow": {Worktree: workflowstore.WorktreeConfig{BaseBranch: "main"}},
		}},
		git:    &gittest.Checkout{},
		sizes:  &fakeWorktreeStore{stored: map[string]worktreestore.Size{}, records: map[string]worktreestore.Worktree{}},
		claims: &fakeClaims{},
		now:    time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, os.MkdirAll(filepath.Join(f.repo, ".git", "worktrees"), 0o755))
	require.NoError(t, os.MkdirAll(f.shared, 0o755))
	f.lister = NewLister(NewListerParams{
		SharedDir: func() (string, error) { return f.shared, nil },
		Agents:    f.owners,
		Workflows: f.flows,
		Sizes:     f.sizes,
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
	return NewInspector(NewInspectorParams{
		Lister:    f.lister,
		Git:       f.git,
		Workflows: f.flows,
		Sizes:     f.sizes,
		Now:       func() time.Time { return f.now },
	})
}

func (f *fixture) measurer() *Measurer {
	return NewMeasurer(NewMeasurerParams{
		Lister:    f.lister,
		Claimed:   f.claims,
		Worktrees: f.sizes,
		Now:       func() time.Time { return f.now },
	})
}

func (f *fixture) remover() *Remover {
	return NewRemover(NewRemoverParams{
		Lister: f.lister,
		Agents: f.owners,
		Git:    f.git,
	})
}

type fakeOwners struct {
	rows []agentstore.AgentDir
}

func (o *fakeOwners) ListDirs(context.Context) ([]agentstore.AgentDir, error) {
	return o.rows, nil
}

// fakeWorktreeStore holds rows and measurements by directory, remembering the order sizes were
// written in and refusing the directories the test names.
type fakeWorktreeStore struct {
	records map[string]worktreestore.Worktree
	stored  map[string]worktreestore.Size
	written []string
	deleted []string
	refused map[string]error
}

func (s *fakeWorktreeStore) List(context.Context) ([]worktreestore.Worktree, error) {
	dirs := map[string]bool{}
	for dir := range s.records {
		dirs[dir] = true
	}
	for dir := range s.stored {
		dirs[dir] = true
	}

	listed := make([]worktreestore.Worktree, 0, len(dirs))
	for dir := range dirs {
		record, ok := s.records[dir]
		if !ok {
			record = worktreestore.Worktree{Dir: dir}
		}
		if size, ok := s.stored[dir]; ok {
			record.Size = &size
		}
		listed = append(listed, record)
	}

	return listed, nil
}

func (s *fakeWorktreeStore) ListSizes(context.Context) ([]worktreestore.Size, error) {
	sizes := make([]worktreestore.Size, 0, len(s.stored))
	for _, size := range s.stored {
		sizes = append(sizes, size)
	}

	return sizes, nil
}

func (s *fakeWorktreeStore) Upsert(_ context.Context, worktree worktreestore.Worktree) error {
	s.records[worktree.Dir] = worktree

	return nil
}

func (s *fakeWorktreeStore) RecordSize(_ context.Context, size worktreestore.Size) error {
	if err, ok := s.refused[size.Dir]; ok {
		return err
	}

	s.stored[size.Dir] = size
	s.written = append(s.written, size.Dir)

	return nil
}

func (s *fakeWorktreeStore) Delete(_ context.Context, dirs []string) error {
	for _, dir := range dirs {
		delete(s.records, dir)
		delete(s.stored, dir)
	}
	s.deleted = append(s.deleted, dirs...)

	return nil
}

// fakeClaims is the directories an unfinished delete names.
type fakeClaims struct {
	dirs []string
}

func (c *fakeClaims) ListClaimedDirs(context.Context) ([]string, error) {
	return c.dirs, nil
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

func write(t *testing.T, root, path, content string) {
	t.Helper()

	full := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}
