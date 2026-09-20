package worktree

import (
	"context"
	"testing"
	"time"

	"weave/internal/git"
	"weave/internal/store"
	agentstore "weave/internal/store/agent"
	worktreestore "weave/internal/store/worktree"
	"weave/internal/worktrees"
	worktreev1 "weave/proto/gen/worktree/v1"
	worktreev1weave "weave/proto/gen/worktree/v1/worktreev1weave"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeWorktrees answers as the three readers and the queue with one directory, remembering what
// it was asked.
type fakeWorktrees struct {
	listing    worktrees.Listing
	inspection worktrees.Inspection
	removal    worktrees.Removal
	inspected  string
	fetched    bool
	removed    []string
	queued     []string
}

func (f *fakeWorktrees) List(context.Context) (worktrees.Listing, error) {
	return f.listing, nil
}

func (f *fakeWorktrees) Inspect(_ context.Context, dir string, fetch bool) (worktrees.Inspection, error) {
	f.inspected, f.fetched = dir, fetch
	if dir != f.inspection.Worktree.Dir {
		return worktrees.Inspection{}, store.ErrNotFound.Saying("%s is not under the worktree directories", dir)
	}

	return f.inspection, nil
}

func (f *fakeWorktrees) Remove(_ context.Context, dirs []string) (worktrees.Removal, error) {
	f.removed = dirs

	return f.removal, nil
}

func (f *fakeWorktrees) DeleteWorktrees(_ context.Context, dirs []string) error {
	f.queued = dirs

	return nil
}

func newService(fake *fakeWorktrees) worktreev1weave.WorktreeServiceAdapter {
	return worktreev1weave.WorktreeServiceAdapter{Impl: NewWorktreeService(NewWorktreeServiceParams{
		Lister:    fake,
		Inspector: fake,
		Remover:   fake,
		Deleter:   fake,
	})}
}

func listed() worktrees.Worktree {
	archived := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	return worktrees.Worktree{
		Dir:      "/trees/aaaa",
		RepoDir:  "/repo",
		Branch:   "agent/aaaa",
		Readable: true,
		Agent: &worktrees.Agent{
			ID:         "aaaa",
			Name:       "first",
			State:      agentstore.StateIdle,
			ArchivedAt: &archived,
			Adopted:    true,
		},
		Occupants: []worktrees.Agent{{ID: "bbbb", Name: "first-reviewer", State: agentstore.StateThinking}},
		Size: &worktreestore.Size{
			Dir:        "/trees/aaaa",
			Bytes:      49 * 1024 * 1024,
			MeasuredAt: archived.Add(time.Hour),
		},
		LastActiveAt: archived,
	}
}

// TestListWorktrees covers the listing rendered whole: the agent with its stamp, the
// occupants, the size and when it was measured, the last activity, a failed measurement
// carrying its error and no size, an unmeasured directory carrying neither, and the
// directories walked.
func TestListWorktrees(t *testing.T) {
	t.Parallel()

	failed := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	fake := &fakeWorktrees{listing: worktrees.Listing{
		Worktrees: []worktrees.Worktree{
			listed(),
			{
				Dir:          "/trees/gone",
				RepoDir:      "/was",
				LastActiveAt: failed.Add(-time.Hour),
				Size: &worktreestore.Size{
					Dir:        "/trees/gone",
					MeasuredAt: failed,
					Error:      "permission denied",
				},
			},
			{Dir: "/trees/new", RepoDir: "/repo"},
		},
		Dirs:           []string{"/trees", "/own"},
		UnreadableDirs: []string{"/own"},
	}}

	resp, err := newService(fake).ListWorktrees(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)

	require.Len(t, resp.Msg.GetWorktrees(), 3)
	first := resp.Msg.GetWorktrees()[0]
	assert.Equal(t, "/trees/aaaa", first.GetDir())
	assert.Equal(t, "agent/aaaa", first.GetBranch())
	assert.True(t, first.GetReadable())
	assert.Equal(t, "first", first.GetAgent().GetName())
	assert.Equal(t, "idle", first.GetAgent().GetState())
	assert.Equal(t, "2026-09-19T12:00:00Z", first.GetAgent().GetArchivedAt())
	assert.True(t, first.GetAgent().GetAdopted())
	require.Len(t, first.GetOccupants(), 1)
	assert.Equal(t, "first-reviewer", first.GetOccupants()[0].GetName())
	require.NotNil(t, first.SizeBytes)
	assert.Equal(t, int64(49*1024*1024), first.GetSizeBytes())
	assert.Equal(t, "2026-09-19T13:00:00Z", first.GetMeasuredAt())
	assert.Empty(t, first.GetSizeError())
	assert.Equal(t, "2026-09-19T12:00:00Z", first.GetLastActiveAt())

	gone := resp.Msg.GetWorktrees()[1]
	assert.Nil(t, gone.Agent)
	assert.False(t, gone.GetReadable())
	assert.Nil(t, gone.SizeBytes, "a failed measurement carries no size")
	assert.Equal(t, "2026-09-20T09:00:00Z", gone.GetMeasuredAt())
	assert.Equal(t, "permission denied", gone.GetSizeError())
	assert.Equal(t, "2026-09-20T08:00:00Z", gone.GetLastActiveAt())

	unmeasured := resp.Msg.GetWorktrees()[2]
	assert.Nil(t, unmeasured.SizeBytes)
	assert.Nil(t, unmeasured.MeasuredAt)
	assert.Empty(t, unmeasured.GetLastActiveAt())

	assert.Equal(t, []string{"/trees", "/own"}, resp.Msg.GetDirs())
	assert.Equal(t, []string{"/own"}, resp.Msg.GetUnreadableDirs())
}

// TestGetWorktree covers the request reaching the inspector as asked and the standing
// rendered whole.
func TestGetWorktree(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 19, 11, 0, 0, 0, time.UTC)
	fake := &fakeWorktrees{inspection: worktrees.Inspection{
		Worktree:           listed(),
		Bytes:              42,
		Entries:            []worktrees.Entry{{Path: "local", Bytes: 40, Dir: true}, {Path: "go.mod", Bytes: 2}},
		Changes:            git.Changes{Modified: []string{"a.go"}, Untracked: []string{"b.txt"}},
		BaseRef:            "origin/main",
		Ahead:              2,
		Behind:             12,
		Landed:             1,
		SequenceInProgress: true,
		KeepsBranch:        true,
		Commits:            []git.Commit{{SHA: "abc", Subject: "own work", CommittedAt: at}},
	}}

	resp, err := newService(fake).GetWorktree(t.Context(), connect.NewRequest(&worktreev1.GetWorktreeRequest{Dir: "/trees/aaaa", Fetch: true}))
	require.NoError(t, err)

	assert.Equal(t, "/trees/aaaa", fake.inspected)
	assert.True(t, fake.fetched)
	assert.Equal(t, "/trees/aaaa", resp.Msg.GetWorktree().GetDir())
	assert.Equal(t, int64(42), resp.Msg.GetSizeBytes())
	require.Len(t, resp.Msg.GetEntries(), 2)
	assert.Equal(t, "local", resp.Msg.GetEntries()[0].GetPath())
	assert.True(t, resp.Msg.GetEntries()[0].GetDir())
	assert.Equal(t, []string{"a.go"}, resp.Msg.GetModified())
	assert.Equal(t, []string{"b.txt"}, resp.Msg.GetUntracked())
	assert.Equal(t, "origin/main", resp.Msg.GetBaseRef())
	assert.Equal(t, int32(2), resp.Msg.GetAhead())
	assert.Equal(t, int32(12), resp.Msg.GetBehind())
	assert.Equal(t, int32(1), resp.Msg.GetLandedCommits())
	assert.True(t, resp.Msg.GetSequenceInProgress())
	assert.True(t, resp.Msg.GetKeepsBranch())
	require.Len(t, resp.Msg.GetCommits(), 1)
	assert.Equal(t, "2026-09-19T11:00:00Z", resp.Msg.GetCommits()[0].GetCommittedAt())
}

// TestGetWorktreeNotFound covers the inspector's refusal reaching the caller with its kind.
func TestGetWorktreeNotFound(t *testing.T) {
	t.Parallel()

	fake := &fakeWorktrees{inspection: worktrees.Inspection{Worktree: listed()}}

	_, err := newService(fake).GetWorktree(t.Context(), connect.NewRequest(&worktreev1.GetWorktreeRequest{Dir: "/elsewhere"}))
	require.Error(t, err)
	assert.Equal(t, connect.CodeNotFound, connect.CodeOf(err))
}

// TestRemoveUnownedWorktrees covers the directories reaching the remover and the removal
// coming back with what went, what did not, and why.
func TestRemoveUnownedWorktrees(t *testing.T) {
	t.Parallel()

	fake := &fakeWorktrees{removal: worktrees.Removal{
		Removed: []string{"/trees/gone"},
		Refused: []worktrees.Refusal{{Dir: "/trees/aaaa", Reason: "agent first answers for it; delete the agent instead"}},
	}}

	resp, err := newService(fake).RemoveUnownedWorktrees(t.Context(), connect.NewRequest(&worktreev1.RemoveUnownedWorktreesRequest{Dirs: []string{"/trees/gone", "/trees/aaaa"}}))
	require.NoError(t, err)

	assert.Equal(t, []string{"/trees/gone", "/trees/aaaa"}, fake.removed)
	assert.Equal(t, []string{"/trees/gone"}, resp.Msg.GetRemoved())
	require.Len(t, resp.Msg.GetRefused(), 1)
	assert.Equal(t, "/trees/aaaa", resp.Msg.GetRefused()[0].GetDir())
	assert.Contains(t, resp.Msg.GetRefused()[0].GetReason(), "delete the agent instead")
}

// TestDeleteWorktrees covers the directories reaching the queue as one job and nothing being
// removed on the call.
func TestDeleteWorktrees(t *testing.T) {
	t.Parallel()

	fake := &fakeWorktrees{}

	_, err := newService(fake).DeleteWorktrees(t.Context(), connect.NewRequest(&worktreev1.DeleteWorktreesRequest{Dirs: []string{"/trees/gone", "/trees/aaaa"}}))
	require.NoError(t, err)

	assert.Equal(t, []string{"/trees/gone", "/trees/aaaa"}, fake.queued)
	assert.Empty(t, fake.removed, "the call queues the job and removes nothing itself")
}
