package internaljobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/worktrees"
	"komrad/lib/xptr"

	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWorktrees answers as the listing and the remover, remembering what the remover was asked.
type fakeWorktrees struct {
	listing   worktrees.Listing
	listErr   error
	removeErr error
	refused   []worktrees.Refusal
	removed   [][]string
}

func (f *fakeWorktrees) List(context.Context) (*worktrees.Listing, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	return &f.listing, nil
}

func (f *fakeWorktrees) Remove(_ context.Context, dirs []string) (*worktrees.Removal, error) {
	f.removed = append(f.removed, dirs)
	if f.removeErr != nil {
		return nil, f.removeErr
	}

	return &worktrees.Removal{Removed: dirs, Refused: f.refused}, nil
}

func newDeleteWorktreesWorker(fake *fakeWorktrees) *DeleteWorktreesWorker {
	return NewDeleteWorktreesWorker(NewDeleteWorktreesWorkerParams{
		Worktrees: fake,
		Remover:   fake,
	})
}

func taskWorktree(dir string, task worktrees.Task) worktrees.Worktree {
	return worktrees.Worktree{
		Dir:      dir,
		Readable: true,
		Task:     &task,
	}
}

func deletingWorktree(dir string) worktrees.Worktree {
	return taskWorktree(dir, worktrees.Task{
		Slug:       "going",
		ArchivedAt: xptr.Ptr(time.Now()),
		Deleting:   true,
	})
}

// TestDeleteWorktreesSendsEachDirectoryDownItsPath covers the listing deciding how a directory
// goes, and what the job answers River with.
func TestDeleteWorktreesSendsEachDirectoryDownItsPath(t *testing.T) {
	tests := []struct {
		name        string
		listing     []worktrees.Worktree
		dirs        []string
		refused     []worktrees.Refusal
		removeErr   error
		wantRemoved [][]string
		wantErr     bool
		wantCancel  bool
		wantSaid    string
	}{
		{
			name:    "a directory the listing no longer holds is skipped",
			listing: []worktrees.Worktree{deletingWorktree("/trees/a")},
			dirs:    []string{"/trees/gone"},
		},
		{
			name:       "a live task's directory is refused and the job is cancelled",
			listing:    []worktrees.Worktree{taskWorktree("/trees/live", worktrees.Task{Slug: "worktree-view"})},
			dirs:       []string{"/trees/live"},
			wantCancel: true,
			wantSaid:   "task worktree-view is live; archive it first",
		},
		{
			name:       "an archived task's directory is refused and the job is cancelled",
			listing:    []worktrees.Worktree{taskWorktree("/trees/a", worktrees.Task{Slug: "put-away", ArchivedAt: xptr.Ptr(time.Now())})},
			dirs:       []string{"/trees/a"},
			wantCancel: true,
			wantSaid:   "task put-away holds it; delete the task instead",
		},
		{
			name:        "the remover's refusal is cancelled rather than retried",
			listing:     []worktrees.Worktree{{Dir: "/trees/orphan", Readable: true}},
			dirs:        []string{"/trees/orphan"},
			refused:     []worktrees.Refusal{{Dir: "/trees/orphan", Reason: worktrees.RefusedNestsWorktree, NestedDir: "/trees/orphan/nested"}},
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
				Reason: worktrees.RefusedRemoveFailed,
				Err:    errors.New("permission denied"),
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
			name:    "a directory whose task is being deleted is left to that delete",
			listing: []worktrees.Worktree{deletingWorktree("/trees/a")},
			dirs:    []string{"/trees/a"},
		},
		{
			name:        "a directory nothing answers for goes to the remover",
			listing:     []worktrees.Worktree{{Dir: "/trees/orphan", Readable: true}},
			dirs:        []string{"/trees/orphan"},
			wantRemoved: [][]string{{"/trees/orphan"}},
		},
		{
			name: "both paths in one job",
			listing: []worktrees.Worktree{
				deletingWorktree("/trees/a"),
				{Dir: "/trees/orphan", Readable: true},
			},
			dirs:        []string{"/trees/orphan", "/trees/a"},
			wantRemoved: [][]string{{"/trees/orphan"}},
		},
		{
			name: "a directory refused over a task whose teardown is running is retried",
			listing: []worktrees.Worktree{
				{Dir: "/trees/outer", Readable: true},
				deletingWorktree("/trees/outer/inner"),
			},
			dirs:        []string{"/trees/outer", "/trees/outer/inner"},
			refused:     []worktrees.Refusal{{Dir: "/trees/outer", Reason: worktrees.RefusedNestsWorktree, NestedDir: "/trees/outer/inner"}},
			wantRemoved: [][]string{{"/trees/outer"}},
			wantErr:     true,
			wantSaid:    "a task's teardown is still running under it",
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

			assert.Equal(t, tt.wantRemoved, fake.removed, "what the remover was asked")
		})
	}
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
	assert.Empty(t, fake.removed)
}

// TestDescribeRefusal covers the sentence the job logs for every reason the remover gives.
func TestDescribeRefusal(t *testing.T) {
	broke := errors.New("git is not on the path")
	tests := []struct {
		name    string
		refusal worktrees.Refusal
		want    string
	}{
		{
			name:    "outside the worktree directories",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedOutside},
			want:    "it is not under the worktree directories",
		},
		{
			name:    "missing from the listing",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedUnlisted},
			want:    "the listing does not hold it",
		},
		{
			name:    "held by its task",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedTaskHolds, TaskSlug: "put-away"},
			want:    "task put-away holds it; delete the task instead",
		},
		{
			name:    "a live task's",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedTaskLive, TaskSlug: "worktree-view"},
			want:    "task worktree-view is live; archive it first",
		},
		{
			name:    "a task being deleted",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedTaskDeleting, TaskSlug: "going"},
			want:    "task going is being deleted, which takes it",
		},
		{
			name:    "an agent inside",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedAgentInside, AgentName: "reviewer"},
			want:    "agent reviewer is working in it; delete the agent instead",
		},
		{
			name:    "an agent beneath",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedAgentBeneath, AgentName: "under-agent"},
			want:    "agent under-agent is working under it; delete the agent instead",
		},
		{
			name:    "another worktree inside",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedNestsWorktree, NestedDir: "/trees/outer/inner"},
			want:    "it holds the worktree /trees/outer/inner; remove that first",
		},
		{
			name:    "a path that could not be resolved",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedUnresolved, Err: errors.New(`failed to resolve "/trees/missing"`)},
			want:    `failed to resolve "/trees/missing"`,
		},
		{
			name:    "registrations git could not list",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedListFailed, Err: broke},
			want:    "could not list the worktrees of its repository: git is not on the path",
		},
		{
			name:    "a lock git could not lift",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedUnlockFailed, Err: broke},
			want:    "could not unlock it: git is not on the path",
		},
		{
			name:    "a directory that could not be removed",
			refusal: worktrees.Refusal{Reason: worktrees.RefusedRemoveFailed, Err: errors.New("permission denied")},
			want:    "could not remove it: permission denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, describeRefusal(tt.refusal))
		})
	}
}
