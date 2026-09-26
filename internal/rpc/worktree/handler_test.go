package worktree

import (
	"context"
	"errors"
	"testing"
	"time"

	"komrad/internal/git"
	"komrad/internal/store"
	agentstore "komrad/internal/store/agent"
	worktreestore "komrad/internal/store/worktree"
	"komrad/internal/worktrees"
	agentv1 "komrad/proto/gen/agent/v1"
	worktreev1 "komrad/proto/gen/worktree/v1"
	worktreev1komrad "komrad/proto/gen/worktree/v1/worktreev1komrad"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"
)

// fakeWorktrees answers as the readers and the queue, refusing with err when it is set.
type fakeWorktrees struct {
	listing    worktrees.Listing
	inspection worktrees.Inspection
	removal    worktrees.Removal
	err        error
	claimErr   error
	inspected  string
	fetched    bool
	removed    []string
	queued     []string
	claimed    []string
}

func (f *fakeWorktrees) List(context.Context) (*worktrees.Listing, error) {
	if f.err != nil {
		return nil, f.err
	}

	return &f.listing, nil
}

func (f *fakeWorktrees) Inspect(_ context.Context, params worktrees.InspectParams) (*worktrees.Inspection, error) {
	f.inspected, f.fetched = params.Dir, params.Fetch
	if f.err != nil {
		return nil, f.err
	}

	return &f.inspection, nil
}

func (f *fakeWorktrees) Remove(_ context.Context, dirs []string) (*worktrees.Removal, error) {
	f.removed = dirs
	if f.err != nil {
		return nil, f.err
	}

	return &f.removal, nil
}

func (f *fakeWorktrees) DeleteWorktrees(_ context.Context, dirs []string) error {
	f.queued = dirs

	return f.err
}

func (f *fakeWorktrees) ListClaimedDirs(context.Context) ([]string, error) {
	return f.claimed, f.claimErr
}

// TestListWorktrees covers the listing rendered whole: the agent with its stamp, the
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
	assert.Equal(t, "first", first.GetAgent().GetName())
	assert.Equal(t, agentv1.AgentState_AGENT_STATE_IDLE, first.GetAgent().GetState())
	assert.Equal(t, "2026-09-19T12:00:00Z", first.GetAgent().GetArchivedAt())
	assert.True(t, first.GetAgent().GetAdopted())
	require.Len(t, first.GetOccupants(), 1)
	assert.Equal(t, "first-reviewer", first.GetOccupants()[0].GetName())
	require.NotNil(t, first.SizeBytes)
	assert.Equal(t, int64(49*1024*1024), first.GetSizeBytes())
	assert.Equal(t, "2026-09-19T13:00:00Z", first.GetMeasuredAt())
	assert.Empty(t, first.GetSizeError())
	assert.Equal(t, "2026-09-19T12:00:00Z", first.GetLastActiveAt())
	assert.Equal(t, worktreev1.WorktreeFate_WORKTREE_FATE_ARCHIVED, first.GetFate())

	gone := resp.Msg.GetWorktrees()[1]
	assert.Nil(t, gone.Agent)
	assert.Equal(t, worktreev1.WorktreeFate_WORKTREE_FATE_UNOWNED, gone.GetFate())
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
		Worktree: buildListedWorktree(),
		Bytes:    42,
		Entries: []worktrees.Entry{{
			Path:  "local",
			Bytes: 40,
			Dir:   true,
		}, {Path: "go.mod", Bytes: 2}},
		Changes:            git.Changes{Modified: []string{"a.go"}, Untracked: []string{"b.txt"}},
		BaseRef:            "origin/main",
		Ahead:              2,
		Behind:             12,
		Landed:             1,
		SequenceInProgress: true,
		KeepsBranch:        true,
		DiffStat:           &git.DiffStat{Added: 214, Removed: 87},
		Remote:             git.RemoteStanding{Pushed: true, Unpushed: 1, Unpulled: 3},
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
	require.Len(t, resp.Msg.GetCommits(), 1)
	assert.Equal(t, "2026-09-19T11:00:00Z", resp.Msg.GetCommits()[0].GetCommittedAt())
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

// TestDeleteWorktrees covers the directories reaching the queue and nothing being removed on the call.
func TestDeleteWorktrees(t *testing.T) {
	t.Parallel()

	fake := &fakeWorktrees{}

	_, err := newService(fake).DeleteWorktrees(t.Context(), connect.NewRequest(&worktreev1.DeleteWorktreesRequest{Dirs: []string{"/trees/gone", "/trees/aaaa"}}))
	require.NoError(t, err)

	assert.Equal(t, []string{"/trees/gone", "/trees/aaaa"}, fake.queued)
	assert.Empty(t, fake.removed)
}

// TestHandlerAnswersWithTheDomainsKind covers each procedure answering with the code of what the domain said.
func TestHandlerAnswersWithTheDomainsKind(t *testing.T) {
	t.Parallel()

	notUnder := store.ErrNotFound.Saying("/elsewhere is not under the worktree directories")
	broke := errors.New("the disk is gone")

	tests := []struct {
		name     string
		fake     *fakeWorktrees
		call     func(ctx context.Context, service worktreev1komrad.WorktreeServiceAdapter) error
		wantCode connect.Code
	}{
		{
			name: "a listing that fails",
			fake: &fakeWorktrees{err: broke},
			call: func(ctx context.Context, service worktreev1komrad.WorktreeServiceAdapter) error {
				_, err := service.ListWorktrees(ctx, connect.NewRequest(&emptypb.Empty{}))

				return err
			},
			wantCode: connect.CodeInternal,
		},
		{
			name: "a claim read that fails",
			fake: &fakeWorktrees{claimErr: broke},
			call: func(ctx context.Context, service worktreev1komrad.WorktreeServiceAdapter) error {
				_, err := service.ListWorktrees(ctx, connect.NewRequest(&emptypb.Empty{}))

				return err
			},
			wantCode: connect.CodeInternal,
		},
		{
			name: "a directory outside the worktree directories",
			fake: &fakeWorktrees{err: notUnder},
			call: func(ctx context.Context, service worktreev1komrad.WorktreeServiceAdapter) error {
				_, err := service.GetWorktree(ctx, connect.NewRequest(&worktreev1.GetWorktreeRequest{Dir: "/elsewhere"}))

				return err
			},
			wantCode: connect.CodeNotFound,
		},
		{
			name: "a removal that fails",
			fake: &fakeWorktrees{err: broke},
			call: func(ctx context.Context, service worktreev1komrad.WorktreeServiceAdapter) error {
				_, err := service.RemoveUnownedWorktrees(ctx, connect.NewRequest(&worktreev1.RemoveUnownedWorktreesRequest{Dirs: []string{"/trees/gone"}}))

				return err
			},
			wantCode: connect.CodeInternal,
		},
		{
			name: "a delete the queue refuses",
			fake: &fakeWorktrees{err: broke},
			call: func(ctx context.Context, service worktreev1komrad.WorktreeServiceAdapter) error {
				_, err := service.DeleteWorktrees(ctx, connect.NewRequest(&worktreev1.DeleteWorktreesRequest{Dirs: []string{"/trees/gone"}}))

				return err
			},
			wantCode: connect.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call(t.Context(), newService(tt.fake))

			assert.Equal(t, tt.wantCode, connect.CodeOf(err))
		})
	}
}

func newService(fake *fakeWorktrees) worktreev1komrad.WorktreeServiceAdapter {
	return worktreev1komrad.WorktreeServiceAdapter{Impl: NewHandler(NewHandlerParams{
		Lister:    fake,
		Inspector: fake,
		Remover:   fake,
		Deleter:   fake,
		Claimed:   fake,
	})}
}

func buildListedWorktree() worktrees.Worktree {
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
		Occupants: []worktrees.Agent{{
			ID:    "bbbb",
			Name:  "first-reviewer",
			State: agentstore.StateThinking,
		}},
		Size: &worktreestore.Size{
			Dir:        "/trees/aaaa",
			Bytes:      49 * 1024 * 1024,
			MeasuredAt: archived.Add(time.Hour),
		},
		LastActiveAt: archived,
	}
}
