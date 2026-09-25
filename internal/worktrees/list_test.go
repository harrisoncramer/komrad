package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"komrad/internal/gittest"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
	"komrad/internal/storetest"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListReadsADirectoryWithoutGit covers the repository, the branch and the readability of
// a worktree coming off its own files, and the agent row that answers for it.
func TestListReadsADirectoryWithoutGit(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	agent := fixture.own(t, dir, agentstore.Agent{Name: "first"})
	fixture.archive(t, agent.ID)

	listing, err := fixture.lister.List(t.Context())
	require.NoError(t, err)

	require.Len(t, listing.Worktrees, 1)
	tree := listing.Worktrees[0]
	assert.Equal(t, dir, tree.Dir)
	assert.Equal(t, fixture.repo, tree.RepoDir)
	assert.Equal(t, "agent/"+agent.ID, tree.Branch)
	assert.True(t, tree.Readable)
	require.NotNil(t, tree.Agent)
	assert.Equal(t, "first", tree.Agent.Name)
	assert.Equal(t, agentstore.StateIdle, tree.Agent.State)
	assert.NotNil(t, tree.Agent.ArchivedAt)
	assert.Equal(t, fixture.workflowID, tree.Agent.WorkflowID)
	assert.Equal(t, []string{fixture.shared}, listing.Dirs)
	assert.Empty(t, listing.UnreadableDirs)
}

// TestListReadsWhatADirectorySaysOfItself covers directories that are not a readable worktree on a branch.
func TestListReadsWhatADirectorySaysOfItself(t *testing.T) {
	tests := []struct {
		name string
		// lay returns the repository the directory should read as cut from.
		lay          func(t *testing.T, fixture *worktreeFixture) string
		wantReadable bool
	}{
		{
			name: "a directory with no .git file",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				require.NoError(t, os.MkdirAll(filepath.Join(fixture.shared, "cccc"), 0o755))
				write(t, fixture.shared, "a-file", "not a directory\n")

				return ""
			},
		},
		{
			name: "a .git file naming a gitdir that is gone still reads the repository off the path",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				write(t, filepath.Join(fixture.shared, "bbbb"), ".git", "gitdir: /var/folders/xx/T/repo/.git/worktrees/bbbb\n")

				return "/var/folders/xx/T/repo"
			},
		},
		{
			name: "a detached head names no branch",
			lay: func(t *testing.T, fixture *worktreeFixture) string {
				dir := fixture.cut(t)
				write(t, filepath.Join(fixture.repo, ".git", "worktrees", filepath.Base(dir)), "HEAD", "0123456789abcdef0123456789abcdef01234567\n")

				return fixture.repo
			},
			wantReadable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newFixture(t)
			wantRepoDir := tt.lay(t, fixture)

			listing, err := fixture.lister.List(t.Context())
			require.NoError(t, err)

			require.Len(t, listing.Worktrees, 1)
			tree := listing.Worktrees[0]
			assert.Equal(t, wantRepoDir, tree.RepoDir)
			assert.Equal(t, tt.wantReadable, tree.Readable)
			assert.Empty(t, tree.Branch)
			assert.Nil(t, tree.Agent)
		})
	}
}

// TestListNamesTheOccupantsOfADirectory covers every row whose cwd is the directory, however
// the path is spelled, being listed beside the row it was cut for, sorted by name.
func TestListNamesTheOccupantsOfADirectory(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	fixture.own(t, dir, agentstore.Agent{Name: "parent"})
	fixture.occupy(t, dir, "zed-reviewer")
	fixture.occupy(t, dir+string(filepath.Separator), "amy-alone")

	listing, err := fixture.lister.List(t.Context())
	require.NoError(t, err)

	require.Len(t, listing.Worktrees, 1)
	require.NotNil(t, listing.Worktrees[0].Agent)
	assert.Equal(t, "parent", listing.Worktrees[0].Agent.Name)
	require.Len(t, listing.Worktrees[0].Occupants, 2)
	assert.Equal(t, "amy-alone", listing.Worktrees[0].Occupants[0].Name)
	assert.Equal(t, "zed-reviewer", listing.Worktrees[0].Occupants[1].Name)
}

// TestListWalksEachWorkflowsOwnDirectory covers a workflow's own directory being walked beside the shared one.
func TestListWalksEachWorkflowsOwnDirectory(t *testing.T) {
	fixture := newFixture(t)
	own := filepath.Join(t.TempDir(), "own")
	require.NoError(t, os.MkdirAll(filepath.Join(own, "eeee"), 0o755))
	for name, dir := range map[string]string{
		"own":         own,
		"own-spelled": own + string(filepath.Separator),
		"missing":     filepath.Join(t.TempDir(), "missing"),
	} {
		fixture.stores.SeedWorkflow(t, workflowstore.Config{Name: name, Worktree: workflowstore.WorktreeConfig{Dir: dir}})
	}
	fixture.cut(t)

	listing, err := fixture.lister.List(t.Context())
	require.NoError(t, err)

	assert.Len(t, listing.Worktrees, 2)
	assert.Len(t, listing.Dirs, 3)
	assert.Empty(t, listing.UnreadableDirs)
}

// TestListNamesADirectoryItCannotRead covers the shared directory being unreadable being
// reported rather than read as empty.
func TestListNamesADirectoryItCannotRead(t *testing.T) {
	fixture := newFixture(t)
	unreadable := filepath.Join(filepath.Dir(fixture.shared), "not-a-dir")
	write(t, filepath.Dir(fixture.shared), "not-a-dir", "x\n")

	listing, err := fixture.newListerOver(unreadable).List(t.Context())
	require.NoError(t, err)

	assert.Empty(t, listing.Worktrees)
	assert.Equal(t, []string{unreadable}, listing.UnreadableDirs)
}

// TestListJoinsTheStoredSize covers a directory carrying the measurement stored for it, and one
// the loop has not reached carrying none.
func TestListJoinsTheStoredSize(t *testing.T) {
	fixture := newFixture(t)
	measured := fixture.cut(t)
	unmeasured := fixture.cut(t)
	fixture.recordSize(t, measured, fixture.now)

	listing, err := fixture.lister.List(t.Context())
	require.NoError(t, err)

	require.Len(t, listing.Worktrees, 2)
	sizes := map[string]bool{}
	for _, tree := range listing.Worktrees {
		sizes[tree.Dir] = tree.Size != nil
	}
	assert.Equal(t, map[string]bool{measured: true, unmeasured: false}, sizes)
}

// TestGetRefusesADirectoryTheListingDoesNotHold covers a path outside the worktree directories being not found.
func TestGetRefusesADirectoryTheListingDoesNotHold(t *testing.T) {
	tests := []struct {
		name string
		dir  func(fixture *worktreeFixture) string
	}{
		{
			name: "a path outside the worktree directories",
			dir: func(fixture *worktreeFixture) string {
				return fixture.repo
			},
		},
		{
			name: "a directory that is not there",
			dir: func(fixture *worktreeFixture) string {
				return filepath.Join(fixture.shared, "cccc")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newFixture(t)
			fixture.cut(t)

			_, err := fixture.lister.Get(t.Context(), tt.dir(fixture))

			require.ErrorIs(t, err, store.ErrNotFound)
		})
	}
}

// TestGetReadsOneDirectory covers one directory answered with its row and its size under a
// worktree directory configured with a trailing slash.
func TestGetReadsOneDirectory(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	fixture.cut(t)
	agent := fixture.own(t, dir, agentstore.Agent{Name: "first"})
	fixture.recordSize(t, dir, fixture.now)
	lister := fixture.newListerOver(fixture.shared + string(filepath.Separator))

	tree, err := lister.Get(t.Context(), dir+string(filepath.Separator))
	require.NoError(t, err)

	assert.Equal(t, dir, tree.Dir)
	assert.Equal(t, "agent/"+agent.ID, tree.Branch)
	require.NotNil(t, tree.Agent)
	assert.Equal(t, "first", tree.Agent.Name)
	require.NotNil(t, tree.Size)
	assert.Equal(t, int64(1), tree.Size.Bytes)
}

// TestLastActiveIsTheLaterOfTheAgentsTimes covers when a directory was last touched.
func TestLastActiveIsTheLaterOfTheAgentsTimes(t *testing.T) {
	earlier := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	later := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		worktree Worktree
		want     time.Time
	}{
		{
			name:     "no agent reads the directory's mtime",
			worktree: Worktree{ModifiedAt: earlier},
			want:     earlier,
		},
		{
			name: "an agent archived after its last state change",
			worktree: Worktree{Agent: &Agent{
				StateChangedAt: earlier,
				ArchivedAt:     &later,
			}},
			want: later,
		},
		{
			name: "an agent whose state changed after it was archived",
			worktree: Worktree{Agent: &Agent{
				StateChangedAt: later,
				ArchivedAt:     &earlier,
			}},
			want: later,
		},
		{
			name:     "a live agent reads its state change",
			worktree: Worktree{Agent: &Agent{StateChangedAt: earlier}},
			want:     earlier,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, lastActive(tt.worktree))
		})
	}
}

// TestModifiedAtPrefersTheGitFile covers the .git file's mtime winning over the directory's.
func TestModifiedAtPrefersTheGitFile(t *testing.T) {
	gitTouched := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	dirTouched := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		hasGitFile bool
		want       time.Time
	}{
		{
			name: "a directory with no .git file reads its own",
			want: dirTouched,
		},
		{
			name:       "a directory with a .git file reads the file's",
			hasGitFile: true,
			want:       gitTouched,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.hasGitFile {
				write(t, dir, ".git", "gitdir: /elsewhere\n")
				require.NoError(t, os.Chtimes(filepath.Join(dir, ".git"), gitTouched, gitTouched))
			}
			require.NoError(t, os.Chtimes(dir, dirTouched, dirTouched))

			assert.True(t, modifiedAt(dir).Equal(tt.want), "got %s, want %s", modifiedAt(dir), tt.want)
		})
	}
}

// TestFateIsDecidedByTheRow covers which delete path each directory takes.
func TestFateIsDecidedByTheRow(t *testing.T) {
	archived := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		worktree Worktree
		want     Fate
	}{
		{
			name:     "a directory nothing answers for",
			worktree: Worktree{Readable: true},
			want:     FateUnowned,
		},
		{
			name: "a row whose delete failed on a directory git cannot read",
			worktree: Worktree{Agent: &Agent{
				FailedToCleanup: true,
			}},
			want: FateUnowned,
		},
		{
			name: "a row whose delete failed on a directory git still reads",
			worktree: Worktree{
				Readable: true,
				Agent:    &Agent{FailedToCleanup: true},
			},
			want: FateLive,
		},
		{
			name: "an archived row",
			worktree: Worktree{
				Readable: true,
				Agent:    &Agent{ArchivedAt: &archived},
			},
			want: FateArchived,
		},
		{
			name: "a live row",
			worktree: Worktree{
				Readable: true,
				Agent:    &Agent{},
			},
			want: FateLive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.worktree.Fate())
		})
	}
}

// TestUnclaimedLeavesOutWhatADeleteHasClaimed covers which directories a delete has claimed.
func TestUnclaimedLeavesOutWhatADeleteHasClaimed(t *testing.T) {
	archived := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)
	owned := Worktree{
		Dir:      "/trees/owned",
		Readable: true,
		Agent:    &Agent{ArchivedAt: &archived},
	}
	unowned := Worktree{Dir: "/trees/unowned", Readable: true}

	tests := []struct {
		name    string
		listing []Worktree
		claimed []string
		want    []string
	}{
		{
			name: "an agent a delete has claimed",
			listing: []Worktree{
				{
					Dir:      "/trees/deleting",
					Readable: true,
					Agent:    &Agent{Deleting: true},
				},
				unowned,
			},
			want: []string{"/trees/unowned"},
		},
		{
			name:    "a directory nothing answers for that a delete names",
			listing: []Worktree{owned, unowned},
			claimed: []string{"/trees/unowned"},
			want:    []string{"/trees/owned"},
		},
		{
			name: "a directory whose agent's delete failed that a delete names",
			listing: []Worktree{
				owned,
				{
					Dir:   "/trees/stuck",
					Agent: &Agent{FailedToCleanup: true},
				},
			},
			claimed: []string{"/trees/stuck"},
			want:    []string{"/trees/owned"},
		},
		{
			name:    "an archived agent's directory that a delete names",
			listing: []Worktree{owned},
			claimed: []string{"/trees/owned"},
			want:    []string{"/trees/owned"},
		},
		{
			name:    "nothing claimed",
			listing: []Worktree{owned, unowned},
			want:    []string{"/trees/owned", "/trees/unowned"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := xslice.Map(Unclaimed(tt.listing, tt.claimed), func(worktree Worktree) string {
				return worktree.Dir
			})

			assert.Equal(t, tt.want, got)
		})
	}
}

// worktreeFixture is a hand-laid repository with a worktrees directory beside it, over a real store.
type worktreeFixture struct {
	repo       string
	shared     string
	stores     *storetest.Stores
	worktrees  *worktreestore.WorktreeStore
	workflowID string
	git        *gittest.Checkout
	claims     *fakeClaimedDirLister
	lister     *Lister
	now        time.Time
}

// fakeClaimedDirLister answers with the directories an unfinished delete names.
type fakeClaimedDirLister struct {
	dirs []string
}

func (c *fakeClaimedDirLister) ListClaimedDirs(context.Context) ([]string, error) {
	return c.dirs, nil
}

// fakeWorktreeLister answers with whatever listing the test names, on disk or not.
type fakeWorktreeLister struct {
	listing Listing
}

func (l *fakeWorktreeLister) List(context.Context) (*Listing, error) {
	return &l.listing, nil
}

func newFixture(t *testing.T) *worktreeFixture {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	stores := storetest.Open(t)
	fixture := &worktreeFixture{
		repo:       filepath.Join(root, "repo"),
		shared:     filepath.Join(root, "trees"),
		stores:     stores,
		worktrees:  worktreestore.NewWorktreeStore(stores.Pool.GetDB()),
		workflowID: stores.SeedWorkflow(t, workflowstore.Config{Name: "flow"}).ID,
		git:        &gittest.Checkout{},
		claims:     &fakeClaimedDirLister{},
		now:        time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
	}
	require.NoError(t, os.MkdirAll(filepath.Join(fixture.repo, ".git", "worktrees"), 0o755))
	require.NoError(t, os.MkdirAll(fixture.shared, 0o755))
	fixture.lister = fixture.newListerOver(fixture.shared)

	return fixture
}

func (f *worktreeFixture) newListerOver(shared string) *Lister {
	return NewLister(NewListerParams{
		SharedDir: func() (string, error) {
			return shared, nil
		},
		Agents:    f.stores.Agents,
		Workflows: f.stores.Workflows,
		Sizes:     f.worktrees,
	})
}

// cut lays out a worktree named with a fresh agent id, with HEAD on the agent's branch.
func (f *worktreeFixture) cut(t *testing.T) string {
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

func (f *worktreeFixture) own(t *testing.T, dir string, agent agentstore.Agent) *agentstore.Agent {
	t.Helper()

	agent.ID = filepath.Base(dir)
	agent.Cwd = dir
	if agent.WorkflowID == "" {
		agent.WorkflowID = f.workflowID
	}

	return f.stores.SeedAgent(t, agent)
}

// occupy records an agent working in a directory it was not made for.
func (f *worktreeFixture) occupy(t *testing.T, cwd, name string) {
	t.Helper()

	f.stores.SeedAgent(t, agentstore.Agent{
		Name:       name,
		Cwd:        cwd,
		WorkflowID: f.workflowID,
	})
}

func (f *worktreeFixture) archive(t *testing.T, id string) {
	t.Helper()

	_, err := f.stores.Agents.Archive(t.Context(), []string{id})
	require.NoError(t, err)
}

func (f *worktreeFixture) recordSize(t *testing.T, dir string, at time.Time) {
	t.Helper()

	require.NoError(t, f.worktrees.Upsert(t.Context(), worktreestore.Worktree{Dir: dir}))
	require.NoError(t, f.worktrees.RecordSize(t.Context(), worktreestore.Size{
		Dir:        dir,
		Bytes:      1,
		MeasuredAt: at,
	}))
}

func (f *worktreeFixture) listStored(t *testing.T) map[string]worktreestore.Worktree {
	t.Helper()

	listed, err := f.worktrees.List(t.Context())
	require.NoError(t, err)

	return xslice.KeyBy(listed, func(worktree worktreestore.Worktree) string {
		return worktree.Dir
	})
}

func (f *worktreeFixture) listMeasuredAt(t *testing.T, at time.Time) []string {
	t.Helper()

	var dirs []string
	for dir, worktree := range f.listStored(t) {
		if worktree.Size != nil && worktree.Size.MeasuredAt.Equal(at) {
			dirs = append(dirs, dir)
		}
	}

	return dirs
}

func (f *worktreeFixture) readSize(t *testing.T, dir string) worktreestore.Size {
	t.Helper()

	size := f.listStored(t)[dir].Size
	require.NotNil(t, size, "%s has no measurement", dir)

	return *size
}

func (f *worktreeFixture) newInspector() *Inspector {
	return NewInspector(NewInspectorParams{
		Lister:    f.lister,
		Git:       f.git,
		Workflows: f.stores.Workflows,
		Sizes:     f.worktrees,
		Now: func() time.Time {
			return f.now
		},
	})
}

func (f *worktreeFixture) newMeasurer() *Measurer {
	return f.newMeasurerOver(f.lister)
}

func (f *worktreeFixture) newMeasurerOver(lister worktreeLister) *Measurer {
	return NewMeasurer(NewMeasurerParams{
		Lister:    lister,
		Claimed:   f.claims,
		Worktrees: f.worktrees,
		Now: func() time.Time {
			return f.now
		},
	})
}

func (f *worktreeFixture) newRemover() *Remover {
	return NewRemover(NewRemoverParams{
		Lister: f.lister,
		Agents: f.stores.Agents,
		Git:    f.git,
	})
}

func write(t *testing.T, root, path, content string) {
	t.Helper()

	full := filepath.Join(root, filepath.FromSlash(path))
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
}
