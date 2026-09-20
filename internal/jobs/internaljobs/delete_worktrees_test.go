package internaljobs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"weave/internal/worktrees"
	"weave/lib/xptr"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWorktrees answers as the listing, the agent delete and the remover, remembering what
// each was asked.
type fakeWorktrees struct {
	listing   worktrees.Listing
	listErr   error
	deleteErr error
	removeErr error
	refused   []worktrees.Refusal
	deleted   [][]string
	released  [][]string
	removed   [][]string
}

func (f *fakeWorktrees) List(context.Context) (worktrees.Listing, error) {
	return f.listing, f.listErr
}

func (f *fakeWorktrees) DeleteAgents(_ context.Context, ids []string) error {
	f.deleted = append(f.deleted, ids)

	return f.deleteErr
}

func (f *fakeWorktrees) ClearDeleting(_ context.Context, ids []string) error {
	f.released = append(f.released, ids)

	return nil
}

func (f *fakeWorktrees) Remove(_ context.Context, dirs []string) (worktrees.Removal, error) {
	f.removed = append(f.removed, dirs)
	if f.removeErr != nil {
		return worktrees.Removal{}, f.removeErr
	}

	return worktrees.Removal{Removed: dirs, Refused: f.refused}, nil
}

func newDeleteWorktreesWorker(fake *fakeWorktrees) *DeleteWorktreesWorker {
	return NewDeleteWorktreesWorker(NewDeleteWorktreesWorkerParams{
		Worktrees: fake,
		Deleter:   fake,
		Releaser:  fake,
		Remover:   fake,
	})
}

func archivedWorktree(dir, id string) worktrees.Worktree {
	return worktrees.Worktree{
		Dir:      dir,
		Readable: true,
		Agent: &worktrees.Agent{
			ID:         id,
			Name:       "put-away-" + id,
			ArchivedAt: xptr.Ptr(time.Now()),
		},
	}
}

// TestDeleteWorktreesSendsEachDirectoryDownItsPath covers the listing deciding how a directory
// goes, and what the job answers River with.
func TestDeleteWorktreesSendsEachDirectoryDownItsPath(t *testing.T) {
	tests := []struct {
		name         string
		listing      []worktrees.Worktree
		dirs         []string
		refused      []worktrees.Refusal
		deleteErr    error
		removeErr    error
		wantDeleted  [][]string
		wantReleased [][]string
		wantRemoved  [][]string
		wantErr      bool
		wantCancel   bool
		wantSaid     string
	}{
		{
			name:    "a directory the listing no longer holds is skipped",
			listing: []worktrees.Worktree{archivedWorktree("/trees/a", "a")},
			dirs:    []string{"/trees/gone"},
		},
		{
			name: "a live agent's directory is refused and the job is cancelled",
			listing: []worktrees.Worktree{{
				Dir:      "/trees/live",
				Readable: true,
				Agent:    &worktrees.Agent{ID: "live", Name: "worktree-view"},
			}},
			dirs:       []string{"/trees/live"},
			wantCancel: true,
		},
		{
			name:        "the remover's refusal is cancelled rather than retried",
			listing:     []worktrees.Worktree{{Dir: "/trees/orphan", Readable: true}},
			dirs:        []string{"/trees/orphan"},
			refused:     []worktrees.Refusal{{Dir: "/trees/orphan", Reason: "it holds the worktree /trees/orphan/nested; remove that first"}},
			wantRemoved: [][]string{{"/trees/orphan"}},
			wantCancel:  true,
			wantSaid:    "/trees/orphan was left alone: it holds the worktree",
		},
		{
			name:    "a step that broke is retried rather than cancelled",
			listing: []worktrees.Worktree{{Dir: "/trees/orphan", Readable: true}},
			dirs:    []string{"/trees/orphan"},
			refused: []worktrees.Refusal{{
				Dir:    "/trees/orphan",
				Reason: "could not remove it: permission denied",
				Failed: true,
			}},
			wantRemoved: [][]string{{"/trees/orphan"}},
			wantErr:     true,
			wantSaid:    "/trees/orphan: could not remove it",
		},
		{
			name:        "a remover that cannot read the listing is retried",
			listing:     []worktrees.Worktree{{Dir: "/trees/orphan", Readable: true}},
			dirs:        []string{"/trees/orphan"},
			removeErr:   errors.New("the worktrees directory is unreadable"),
			wantRemoved: [][]string{{"/trees/orphan"}},
			wantErr:     true,
		},
		{
			name: "a delete that fails is retried, the refusals riding along in the error",
			listing: []worktrees.Worktree{
				archivedWorktree("/trees/a", "a"),
				{Dir: "/trees/live", Readable: true, Agent: &worktrees.Agent{ID: "live", Name: "worktree-view"}},
			},
			dirs:         []string{"/trees/a", "/trees/live"},
			deleteErr:    errors.New("the store is away"),
			wantDeleted:  [][]string{{"a"}},
			wantReleased: [][]string{{"a"}},
			wantErr:      true,
			wantSaid:     "agent worktree-view is live",
		},
		{
			name:        "an archived agent's directory goes through the agent delete",
			listing:     []worktrees.Worktree{archivedWorktree("/trees/a", "a")},
			dirs:        []string{"/trees/a"},
			wantDeleted: [][]string{{"a"}},
		},
		{
			name:        "a directory nothing answers for goes to the remover",
			listing:     []worktrees.Worktree{{Dir: "/trees/orphan", Readable: true}},
			dirs:        []string{"/trees/orphan"},
			wantRemoved: [][]string{{"/trees/orphan"}},
		},
		{
			name: "a row whose own delete failed on a registration git cannot read goes to the remover",
			listing: []worktrees.Worktree{{
				Dir:   "/trees/stuck",
				Agent: &worktrees.Agent{ID: "stuck", Name: "stuck", FailedToCleanup: true},
			}},
			dirs:        []string{"/trees/stuck"},
			wantRemoved: [][]string{{"/trees/stuck"}},
		},
		{
			name: "both paths in one job",
			listing: []worktrees.Worktree{
				archivedWorktree("/trees/a", "a"),
				{Dir: "/trees/orphan", Readable: true},
			},
			dirs:        []string{"/trees/orphan", "/trees/a"},
			wantDeleted: [][]string{{"a"}},
			wantRemoved: [][]string{{"/trees/orphan"}},
		},
		{
			name: "a directory refused over an agent whose teardown this run started is retried",
			listing: []worktrees.Worktree{
				{Dir: "/trees/outer", Readable: true},
				archivedWorktree("/trees/outer/inner", "inner"),
			},
			dirs:        []string{"/trees/outer", "/trees/outer/inner"},
			refused:     []worktrees.Refusal{{Dir: "/trees/outer", Reason: "it holds the worktree /trees/outer/inner; remove that first"}},
			wantDeleted: [][]string{{"inner"}},
			wantRemoved: [][]string{{"/trees/outer"}},
			wantErr:     true,
			wantSaid:    "an agent's teardown is still running under it",
		},
		{
			name: "a directory refused over an agent whose delete failed is a verdict",
			listing: []worktrees.Worktree{
				{Dir: "/trees/outer", Readable: true},
				archivedWorktree("/trees/outer/inner", "inner"),
			},
			dirs:         []string{"/trees/outer", "/trees/outer/inner"},
			deleteErr:    errors.New("the store is away"),
			refused:      []worktrees.Refusal{{Dir: "/trees/outer", Reason: "it holds the worktree /trees/outer/inner; remove that first"}},
			wantDeleted:  [][]string{{"inner"}},
			wantReleased: [][]string{{"inner"}},
			wantRemoved:  [][]string{{"/trees/outer"}},
			wantErr:      true,
			wantSaid:     "/trees/outer was left alone: it holds the worktree",
		},
		{
			name: "a directory goes before the one holding it, each depth its own call",
			listing: []worktrees.Worktree{
				{Dir: "/trees/outer", Readable: true},
				{Dir: "/trees/outer/inner", Readable: true},
				{Dir: "/trees/other", Readable: true},
			},
			dirs:        []string{"/trees/outer", "/trees/other", "/trees/outer/inner"},
			wantRemoved: [][]string{{"/trees/outer/inner"}, {"/trees/outer", "/trees/other"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeWorktrees{
				listing:   worktrees.Listing{Worktrees: tt.listing},
				refused:   tt.refused,
				deleteErr: tt.deleteErr,
				removeErr: tt.removeErr,
			}
			worker := newDeleteWorktreesWorker(fake)

			err := worker.Work(t.Context(), &river.Job[DeleteWorktreesArgs]{Args: DeleteWorktreesArgs{Dirs: tt.dirs}})

			var cancel *river.JobCancelError
			switch {
			case tt.wantCancel:
				require.ErrorAs(t, err, &cancel)
			case tt.wantErr:
				require.Error(t, err)
				assert.NotErrorAs(t, err, &cancel, "a failure that may pass next time is retried, not cancelled")
			default:
				require.NoError(t, err)
			}
			if tt.wantSaid != "" {
				require.ErrorContains(t, err, tt.wantSaid)
			}

			assert.Equal(t, tt.wantDeleted, fake.deleted, "what the agent delete was asked")
			assert.Equal(t, tt.wantReleased, fake.released, "the agents a refused delete let back into the listings")
			assert.Equal(t, tt.wantRemoved, fake.removed, "what the remover was asked")
		})
	}
}

// TestDeleteWorktreesDeletesAgentsAPageAtATime covers the ids going in the batches the agent
// delete takes.
func TestDeleteWorktreesDeletesAgentsAPageAtATime(t *testing.T) {
	fake := &fakeWorktrees{}
	var dirs []string
	for i := range deleteWorktreesBatch + 2 {
		dir := fmt.Sprintf("/trees/%03d", i)
		fake.listing.Worktrees = append(fake.listing.Worktrees, archivedWorktree(dir, fmt.Sprintf("%03d", i)))
		dirs = append(dirs, dir)
	}
	worker := newDeleteWorktreesWorker(fake)

	require.NoError(t, worker.Work(t.Context(), &river.Job[DeleteWorktreesArgs]{Args: DeleteWorktreesArgs{Dirs: dirs}}))

	require.Len(t, fake.deleted, 2)
	assert.Len(t, fake.deleted[0], deleteWorktreesBatch)
	assert.Len(t, fake.deleted[1], 2)
}

// TestDeleteWorktreesRetriesAListingItCannotRead covers the one failure before anything is
// decided.
func TestDeleteWorktreesRetriesAListingItCannotRead(t *testing.T) {
	fake := &fakeWorktrees{listErr: errors.New("the worktrees directory is unreadable")}
	worker := newDeleteWorktreesWorker(fake)

	err := worker.Work(t.Context(), &river.Job[DeleteWorktreesArgs]{Args: DeleteWorktreesArgs{Dirs: []string{"/trees/a"}}})

	require.Error(t, err)
	var cancel *river.JobCancelError
	assert.NotErrorAs(t, err, &cancel)
	assert.Empty(t, fake.deleted)
	assert.Empty(t, fake.removed)
}
