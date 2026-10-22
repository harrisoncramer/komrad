package worktree

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/git"
	agentstore "komrad/internal/store/agent"
	worktreestore "komrad/internal/store/worktree"
	"komrad/internal/worktrees"
	agentv1 "komrad/proto/gen/agent/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"
	worktreev1komrad "komrad/proto/gen/worktree/v1/worktreev1komrad"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeWorktrees answers as the readers and the queue.
type fakeWorktrees struct {
	listing    worktrees.Listing
	inspection worktrees.Inspection
	inspected  string
	fetched    bool
	removal    worktrees.Removal
	removed    []string
	queued     []string
}

func (f *fakeWorktrees) ListUnclaimed(context.Context) (*worktrees.Listing, error) {
	return &f.listing, nil
}

func (f *fakeWorktrees) Inspect(_ context.Context, params worktrees.InspectParams) (*worktrees.Inspection, error) {
	f.inspected, f.fetched = params.Dir, params.Fetch

	return &f.inspection, nil
}

func (f *fakeWorktrees) Remove(_ context.Context, dirs []string) (*worktrees.Removal, error) {
	f.removed = dirs

	return &f.removal, nil
}

func (f *fakeWorktrees) DeleteWorktrees(_ context.Context, dirs []string) error {
	f.queued = dirs

	return nil
}

// TestListWorktrees covers the listing rendered whole: the task and its stamp, the
// occupants, the size and when it was measured, the last activity, the fate, a failed measurement
// carrying its error and no size, an unmeasured directory carrying neither, and the
// directories walked.
func TestListWorktrees(t *testing.T) {
	t.Parallel()

	failed := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	fake := &fakeWorktrees{listing: worktrees.Listing{
		Worktrees: []worktrees.Worktree{
			buildListedWorktree(),
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
	assert.Equal(t, "aaaa", first.GetTask().GetId())
	assert.Equal(t, "first-task", first.GetTask().GetSlug())
	assert.Equal(t, "2026-09-19T12:00:00Z", first.GetTask().GetArchivedAt())
	assert.True(t, first.GetTask().GetAdopted())
	require.Len(t, first.GetOccupants(), 2)
	assert.Equal(t, "first", first.GetOccupants()[0].GetName())
	assert.Equal(t, agentv1.AgentState_AGENT_STATE_IDLE, first.GetOccupants()[0].GetState())
	assert.Equal(t, "2026-09-19T12:00:00Z", first.GetOccupants()[0].GetArchivedAt())
	assert.Equal(t, "first-reviewer", first.GetOccupants()[1].GetName())
	require.NotNil(t, first.SizeBytes)
	assert.Equal(t, int64(49*1024*1024), first.GetSizeBytes())
	assert.Equal(t, int64(20*1024*1024), first.GetShareBytes())
	assert.Equal(t, "2026-09-19T13:00:00Z", first.GetMeasuredAt())
	assert.Empty(t, first.GetSizeError())
	assert.Equal(t, "2026-09-19T12:00:00Z", first.GetLastActiveAt())
	assert.Equal(t, worktreev1.WorktreeFate_WORKTREE_FATE_ARCHIVED, first.GetFate())

	gone := resp.Msg.GetWorktrees()[1]
	assert.Nil(t, gone.Task)
	assert.Equal(t, worktreev1.WorktreeFate_WORKTREE_FATE_UNOWNED, gone.GetFate())
	assert.False(t, gone.GetReadable())
	assert.Nil(t, gone.SizeBytes, "a failed measurement carries no size")
	assert.Nil(t, gone.ShareBytes)
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
		Worktree: buildListedWorktree(),
		Bytes:    42,
		Entries: []worktrees.Entry{{
			Path:  "local",
			Bytes: 40,
			Dir:   true,
		}, {Path: "go.mod", Bytes: 2}},
		Checkout: worktrees.Checkout{
			Changes:            git.Changes{Modified: []string{"a.go"}, Untracked: []string{"b.txt"}},
			BaseRef:            "origin/main",
			Landed:             1,
			SequenceInProgress: true,
		},
		Ahead:       2,
		Behind:      12,
		KeepsBranch: true,
		DiffStat:    &git.DiffStat{Added: 214, Removed: 87},
		Remote: git.RemoteStanding{
			Pushed:   true,
			Unpushed: 1,
			Unpulled: 3,
		},
		RemoteLanded: true,
		Commits: []git.Commit{{
			SHA:         "abc",
			Subject:     "own work",
			CommittedAt: at,
		}},
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
	assert.Equal(t, int32(214), resp.Msg.GetAddedLines())
	assert.Equal(t, int32(87), resp.Msg.GetRemovedLines())
	assert.True(t, resp.Msg.GetPushed())
	assert.Equal(t, int32(1), resp.Msg.GetUnpushed())
	assert.Equal(t, int32(3), resp.Msg.GetUnpulled())
	assert.True(t, resp.Msg.GetUnpulledLanded())
	require.Len(t, resp.Msg.GetCommits(), 1)
	assert.Equal(t, "2026-09-19T11:00:00Z", resp.Msg.GetCommits()[0].GetCommittedAt())
}

// TestRemoveUnownedWorktrees covers each refusal rendered with its reason and the facts that reason names.
func TestRemoveUnownedWorktrees(t *testing.T) {
	t.Parallel()

	fake := &fakeWorktrees{removal: worktrees.Removal{
		Removed: []string{"/trees/orphan"},
		Refused: []worktrees.Refusal{
			{Dir: "/trees/aaaa", Reason: worktrees.RefusedTaskHolds, TaskSlug: "first-task"},
			{Dir: "/trees/bbbb", Reason: worktrees.RefusedAgentInside, AgentName: "reviewer"},
			{Dir: "/trees/cccc", Reason: worktrees.RefusedNestsWorktree, NestedDir: "/trees/cccc/inner"},
			{Dir: "/trees/dddd", Reason: worktrees.RefusedUnlockFailed, Err: errors.New("git is not on the path")},
		},
	}}

	resp, err := newService(fake).RemoveUnownedWorktrees(t.Context(), connect.NewRequest(&worktreev1.RemoveUnownedWorktreesRequest{
		Dirs: []string{"/trees/orphan", "/trees/aaaa", "/trees/bbbb", "/trees/cccc", "/trees/dddd"},
	}))
	require.NoError(t, err)

	assert.Equal(t, []string{"/trees/orphan"}, resp.Msg.GetRemoved())
	want := []*worktreev1.Refusal{
		{Dir: "/trees/aaaa", Reason: worktreev1.RefusalReason_REFUSAL_REASON_TASK_HOLDS, TaskSlug: "first-task"},
		{Dir: "/trees/bbbb", Reason: worktreev1.RefusalReason_REFUSAL_REASON_AGENT_INSIDE, AgentName: "reviewer"},
		{Dir: "/trees/cccc", Reason: worktreev1.RefusalReason_REFUSAL_REASON_NESTS_WORKTREE, NestedDir: "/trees/cccc/inner"},
		{Dir: "/trees/dddd", Reason: worktreev1.RefusalReason_REFUSAL_REASON_UNLOCK_FAILED, Error: "git is not on the path"},
	}
	assert.Empty(t, cmp.Diff(want, resp.Msg.GetRefused(), protocmp.Transform()))
}

// TestDeleteWorktrees covers the directories reaching the queue and the call removing nothing.
func TestDeleteWorktrees(t *testing.T) {
	t.Parallel()

	fake := &fakeWorktrees{}

	_, err := newService(fake).DeleteWorktrees(t.Context(), connect.NewRequest(&worktreev1.DeleteWorktreesRequest{Dirs: []string{"/trees/gone", "/trees/aaaa"}}))
	require.NoError(t, err)

	assert.Equal(t, []string{"/trees/gone", "/trees/aaaa"}, fake.queued)
	assert.Empty(t, fake.removed)
}

func newService(fake *fakeWorktrees) worktreev1komrad.WorktreeServiceAdapter {
	return worktreev1komrad.WorktreeServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Lister:    fake,
		Inspector: fake,
		Remover:   fake,
		Deleter:   fake,
	})}
}

func buildListedWorktree() worktrees.Worktree {
	archived := time.Date(2026, 9, 19, 12, 0, 0, 0, time.UTC)

	return worktrees.Worktree{
		Dir:      "/trees/aaaa",
		RepoDir:  "/repo",
		Branch:   "agent/aaaa",
		Readable: true,
		Task: &worktrees.Task{
			ID:         firstTaskID,
			Slug:       "first-task",
			Adopted:    true,
			ArchivedAt: &archived,
		},
		Occupants: []worktrees.Agent{
			{
				ID:         firstWorktreeTaskID,
				Name:       "first",
				State:      agentstore.StateIdle,
				ArchivedAt: &archived,
			},
			{
				ID:    uuid.New(),
				Name:  "first-reviewer",
				State: agentstore.StateThinking,
			},
		},
		Size: &worktreestore.Size{
			Dir:        "/trees/aaaa",
			Bytes:      49 * 1024 * 1024,
			Share:      20 * 1024 * 1024,
			MeasuredAt: archived.Add(time.Hour),
		},
		LastActiveAt: archived,
	}
}

var (
	firstTaskID         = uuid.New()
	firstWorktreeTaskID = uuid.New()
)
