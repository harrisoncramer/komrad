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
	"komrad/internal/store/task/tasktest"
	workflowstore "komrad/internal/store/workflow"
	worktreestore "komrad/internal/store/worktree"
	"komrad/internal/storetest"
	"komrad/lib/xptr"
	"komrad/lib/xslice"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListReadsADirectoryWithoutGit covers the repository, the branch and the readability of
// a worktree coming off its own files, the task it was cut for, and the agents in it.
func TestListReadsADirectoryWithoutGit(t *testing.T) {
	fixture := newFixture(t)
	dir := fixture.cut(t)
	agent := fixture.own(t, dir, agentstore.Agent{Name: "first"})
	fixture.archive(t, agent.ID)
	fixture.archiveTask(t, dir)

	listing, err := fixture.lister.List(t.Context())
	require.NoError(t, err)

	require.Len(t, listing.Worktrees, 1)
	tree := listing.Worktrees[0]
	assert.Equal(t, dir, tree.Dir)
	assert.Equal(t, fixture.repo, tree.RepoDir)
	assert.Equal(t, "task/"+*agent.TaskID, tree.Branch)
	assert.True(t, tree.Readable)
	require.NotNil(t, tree.Task)
	assert.Equal(t, *agent.TaskID, tree.Task.ID)
	assert.Equal(t, fixture.workflowID, tree.Task.WorkflowID)
	assert.NotNil(t, tree.Task.ArchivedAt)
	require.Len(t, tree.Occupants, 1)
	assert.Equal(t, "first", tree.Occupants[0].Name)
	assert.Equal(t, agentstore.StateIdle, tree.Occupants[0].State)
	assert.NotNil(t, tree.Occupants[0].ArchivedAt)
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
			assert.Empty(t, tree.Occupants)
		})
	}
}

// TestListNamesTheOccupantsOfADirectory covers which agents a directory lists as working in it,
// live ones first and then newer before older.
func TestListNamesTheOccupantsOfADirectory(t *testing.T) {
	tests := []struct {
		name string
		// seed lays out the agents and returns the names the directory should list.
		seed func(t *testing.T, fixture *worktreeFixture, dir string) []string
	}{
		{
			name: "an agent working in another task's directory is not in this one",
			seed: func(t *testing.T, fixture *worktreeFixture, dir string) []string {
				fixture.occupy(t, fixture.cut(t), "elsewhere")

				return []string{}
			},
		},
		{
			name: "an agent kept in the repository off its task's directory is not in it",
			seed: func(t *testing.T, fixture *worktreeFixture, dir string) []string {
				fixture.stores.SeedAgent(t, agentstore.Agent{
					Name:             "kept",
					Cwd:              fixture.repo,
					WorkflowID:       fixture.workflowID,
					TaskID:           xptr.Ptr(filepath.Base(dir)),
					ProvisionEnabled: xptr.Ptr(false),
				})

				return []string{}
			},
		},
		{
			name: "an agent provisioned for the task is in its directory before it works there",
			seed: func(t *testing.T, fixture *worktreeFixture, dir string) []string {
				fixture.stores.SeedAgent(t, agentstore.Agent{
					Name:       "writer",
					Cwd:        fixture.repo,
					WorkflowID: fixture.workflowID,
					TaskID:     xptr.Ptr(filepath.Base(dir)),
				})

				return []string{"writer"}
			},
		},
		{
			name: "an archived agent comes after a live one",
			seed: func(t *testing.T, fixture *worktreeFixture, dir string) []string {
				fixture.own(t, dir, agentstore.Agent{Name: "live"})
				fixture.archive(t, fixture.own(t, dir, agentstore.Agent{Name: "archived"}).ID)

				return []string{"live", "archived"}
			},
		},
		{
			name: "every agent working in the directory however the path is spelled, newer first",
			seed: func(t *testing.T, fixture *worktreeFixture, dir string) []string {
				fixture.own(t, dir, agentstore.Agent{Name: "parent"})
				fixture.occupy(t, dir, "reviewer")
				fixture.occupy(t, dir+string(filepath.Separator), "alone")

				return []string{"alone", "reviewer", "parent"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newFixture(t)
			dir := fixture.cut(t)
			want := tt.seed(t, fixture, dir)

			tree, err := fixture.lister.Get(t.Context(), dir)
			require.NoError(t, err)

			got := xslice.Map(tree.Occupants, func(agent Agent) string { return agent.Name })
			assert.Equal(t, want, got)
		})
	}
}

// TestListWalksEachWorkflowsOwnDirectory covers the walk reaching a workflow's own directory beside the shared one.
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

// TestListUnclaimedLeavesOutWhatADeleteNames covers the listing dropping a directory an unfinished
// delete names while keeping the directories it walked.
func TestListUnclaimedLeavesOutWhatADeleteNames(t *testing.T) {
	fixture := newFixture(t)
	named := fixture.cutUnowned(t)
	kept := fixture.cut(t)
	fixture.claims.dirs = []string{named}

	listing, err := fixture.lister.ListUnclaimed(t.Context())
	require.NoError(t, err)

	assert.Equal(t, []string{kept}, xslice.Map(listing.Worktrees, func(worktree Worktree) string { return worktree.Dir }))
	assert.Equal(t, []string{fixture.shared}, listing.Dirs)
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
	assert.Equal(t, "task/"+*agent.TaskID, tree.Branch)
	require.NotNil(t, tree.Task)
	assert.Equal(t, *agent.TaskID, tree.Task.ID)
	require.NotNil(t, tree.Size)
	assert.Equal(t, int64(1), tree.Size.Bytes)
}

// TestLastActiveIsTheLatestOfTheOccupantsTimes covers when a directory was last touched.
func TestLastActiveIsTheLatestOfTheOccupantsTimes(t *testing.T) {
	earlier := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	later := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		worktree Worktree
		want     time.Time
	}{
		{
			name:     "no occupant reads the directory's mtime",
			worktree: Worktree{ModifiedAt: earlier},
			want:     earlier,
		},
		{
			name: "an agent archived after its last state change",
			worktree: Worktree{Occupants: []Agent{{
				StateChangedAt: earlier,
				ArchivedAt:     &later,
			}}},
			want: later,
		},
		{
			name: "an agent whose state changed after it was archived",
			worktree: Worktree{Occupants: []Agent{{
				StateChangedAt: later,
				ArchivedAt:     &earlier,
			}}},
			want: later,
		},
		{
			name: "an older occupant touching it last",
			worktree: Worktree{Occupants: []Agent{
				{StateChangedAt: earlier},
				{StateChangedAt: later},
			}},
			want: later,
		},
		{
			name:     "a live agent reads its state change",
			worktree: Worktree{Occupants: []Agent{{StateChangedAt: earlier}}},
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

// TestFateIsDecidedByTheTask covers which delete path each directory takes.
func TestFateIsDecidedByTheTask(t *testing.T) {
	archived := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		worktree Worktree
		want     Fate
	}{
		{
			name:     "a directory no task answers for, whoever works in it",
			worktree: Worktree{Occupants: []Agent{{Name: "stray"}}},
			want:     FateUnowned,
		},
		{
			name: "a directory whose archived task a delete has claimed",
			worktree: Worktree{Task: &Task{
				ArchivedAt: &archived,
				Deleting:   true,
			}},
			want: FateDeleting,
		},
		{
			name:     "a directory whose task is archived, with nobody in it",
			worktree: Worktree{Task: &Task{ArchivedAt: &archived}},
			want:     FateArchived,
		},
		{
			name:     "a directory whose task is open",
			worktree: Worktree{Task: &Task{}},
			want:     FateLive,
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
		Dir:  "/trees/owned",
		Task: &Task{ArchivedAt: &archived},
	}
	unowned := Worktree{Dir: "/trees/unowned"}

	tests := []struct {
		name    string
		listing []Worktree
		claimed []string
		want    []string
	}{
		{
			name: "a directory whose task a delete has claimed",
			listing: []Worktree{
				{
					Dir:  "/trees/deleting",
					Task: &Task{Deleting: true},
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
			name:    "an archived task's directory that a delete names",
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
			got := xslice.Map(unclaimed(tt.listing, tt.claimed), func(worktree Worktree) string {
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
		Tasks:     f.stores.Tasks,
		Workflows: f.stores.Workflows,
		Worktrees: f.worktrees,
		Claimed:   f.claims,
	})
}

// cut lays out a worktree named after a fresh task, with HEAD on the task's branch.
func (f *worktreeFixture) cut(t *testing.T) string {
	t.Helper()

	return f.cutIn(t, f.workflowID)
}

// cutIn lays out a worktree named after a fresh task in the workflow.
func (f *worktreeFixture) cutIn(t *testing.T, workflowID string) string {
	t.Helper()

	id, err := tasktest.Seed(t.Context(), f.stores.Pool.GetDB(), workflowID)
	require.NoError(t, err)

	return f.cutAs(t, *id, "task/"+*id)
}

// cutUnowned lays out a worktree named after a task that has no row.
func (f *worktreeFixture) cutUnowned(t *testing.T) string {
	t.Helper()

	id, err := store.NewID()
	require.NoError(t, err)

	return f.cutAs(t, id, "task/"+id)
}

// cutAs lays out a worktree under a name, with HEAD on a branch.
func (f *worktreeFixture) cutAs(t *testing.T, name, branch string) string {
	t.Helper()

	dir := filepath.Join(f.shared, name)
	gitdir := filepath.Join(f.repo, ".git", "worktrees", name)
	write(t, dir, ".git", "gitdir: "+gitdir+"\n")
	write(t, gitdir, "HEAD", "ref: refs/heads/"+branch+"\n")
	write(t, dir, "own.go", "package main\n")

	return dir
}

// lock writes the lock git keeps in a worktree's gitdir.
func (f *worktreeFixture) lock(t *testing.T, dir string) {
	t.Helper()

	write(t, filepath.Join(f.repo, ".git", "worktrees", filepath.Base(dir)), "locked", "komrad task "+filepath.Base(dir))
}

func (f *worktreeFixture) own(t *testing.T, dir string, agent agentstore.Agent) *agentstore.Agent {
	t.Helper()

	agent.TaskID = xptr.Ptr(filepath.Base(dir))
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

// archiveTask archives the task a directory was cut for.
func (f *worktreeFixture) archiveTask(t *testing.T, dir string) {
	t.Helper()

	require.NoError(t, f.stores.Tasks.Archive(t.Context(), filepath.Base(dir)))
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

func (f *worktreeFixture) readStanding(t *testing.T, dir string) worktreestore.Standing {
	t.Helper()

	standing := f.listStored(t)[dir].Standing
	require.NotNil(t, standing, "%s has no standing", dir)

	return *standing
}

func (f *worktreeFixture) newInspector() *Inspector {
	return NewInspector(NewInspectorParams{
		Lister: f.lister,
		Git:    f.git,
		Checkouts: NewCheckoutReader(NewCheckoutReaderParams{
			Git: f.git,
			Now: func() time.Time {
				return f.now
			},
		}),
		Workflows: f.stores.Workflows,
		Worktrees: f.worktrees,
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
		Standings: f.newInspector(),
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
