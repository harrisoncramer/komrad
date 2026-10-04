package pullrequests

import (
	"errors"
	"testing"
	"time"

	"komrad/internal/forge"
	"komrad/internal/jobs/internaljobs"
	pullrequeststore "komrad/internal/store/pullrequest"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// supersededHeadSHA is the head the pull request supersede leaves behind with its check running.
const supersededHeadSHA = "abc"

// supersededAt is when supersede read the pull request off its old head.
var supersededAt = syncNow.Add(time.Minute)

// supersede reads PR_1 on its old head with its check running, then on a new head with that check passing, and
// answers the pull request's id.
func (f *syncFixture) supersede(t *testing.T) string {
	t.Helper()

	f.agentOn(t, "a", "agent/x")
	running := withMergeReadiness(pullRequestAt("PR_1", "agent/x", syncNow.Add(-time.Hour)), forge.MergeReadiness{
		Decision:     forge.ReviewDecisionPending,
		Mergeability: forge.MergeabilityClean,
		Listing:      listedStatusChecks(forge.ChecksPending),
	})
	f.hold(running)
	f.sync(t)

	moved := withMergeReadiness(running, forge.MergeReadiness{
		Decision:     forge.ReviewDecisionPending,
		Mergeability: forge.MergeabilityClean,
		Listing:      listedStatusChecks(forge.ChecksPassing),
	})
	moved.HeadSHA = "def"
	moved.UpdatedAt = syncNow
	f.forge.pullRequests["harrisoncramer/komrad"] = []forge.PullRequest{moved}
	f.pass(t, supersededAt)
	f.forge.requests = nil

	return f.stored(t, "PR_1").ID
}

// oldCheckState is the state of the check on the superseded head.
func (f *syncFixture) oldCheckState(t *testing.T, pullRequestID string) forge.StatusCheckState {
	t.Helper()

	readings, err := pullrequeststore.NewStatusCheckStore(f.stores.Pool.GetDB()).ListOfPullRequest(t.Context(), pullRequestID)
	require.NoError(t, err)
	for _, reading := range readings {
		if reading.HeadSHA == supersededHeadSHA {
			require.Len(t, reading.StatusChecks, 1)

			return reading.StatusChecks[0].State
		}
	}
	require.Fail(t, "no reading of the superseded head")

	return ""
}

// TestRecoverOldChecks covers each step of reading a superseded head's checks: what it writes, whether it queues
// the next step, and how long it waits when the forge cannot be asked yet.
func TestRecoverOldChecks(t *testing.T) {
	recoverAt := syncNow.Add(5 * time.Minute)
	lastStep := len(internaljobs.RecoverOldChecksDelays) - 1

	tests := []struct {
		name         string
		arrange      func(t *testing.T, f *syncFixture, args *internaljobs.RecoverOldChecksArgs)
		wantState    forge.StatusCheckState
		wantNext     bool
		wantSnooze   time.Duration
		wantRequests []string
	}{
		{
			name: "the checks finished",
			arrange: func(_ *testing.T, f *syncFixture, _ *internaljobs.RecoverOldChecksArgs) {
				f.forge.commitChecks = map[string]*forge.StatusCheckListing{supersededHeadSHA: listedStatusChecks(forge.ChecksPassing)}
			},
			wantState:    forge.StatusCheckStateCompleted,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "a check still running",
			arrange: func(_ *testing.T, f *syncFixture, _ *internaljobs.RecoverOldChecksArgs) {
				f.forge.commitChecks = map[string]*forge.StatusCheckListing{supersededHeadSHA: listedStatusChecks(forge.ChecksPending)}
			},
			wantState:    forge.StatusCheckStateRunning,
			wantNext:     true,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "a check still running on the last step",
			arrange: func(_ *testing.T, f *syncFixture, args *internaljobs.RecoverOldChecksArgs) {
				f.forge.commitChecks = map[string]*forge.StatusCheckListing{supersededHeadSHA: listedStatusChecks(forge.ChecksPending)}
				args.Step = lastStep
			},
			wantState:    forge.StatusCheckStateUnknown,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "the checks finished on the last step",
			arrange: func(_ *testing.T, f *syncFixture, args *internaljobs.RecoverOldChecksArgs) {
				f.forge.commitChecks = map[string]*forge.StatusCheckListing{supersededHeadSHA: listedStatusChecks(forge.ChecksPassing)}
				args.Step = lastStep
			},
			wantState:    forge.StatusCheckStateCompleted,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "a read that lists no checks over a check still running",
			arrange: func(_ *testing.T, f *syncFixture, _ *internaljobs.RecoverOldChecksArgs) {
				f.forge.commitChecks = map[string]*forge.StatusCheckListing{supersededHeadSHA: listedStatusChecks(forge.ChecksNone)}
			},
			wantState:    forge.StatusCheckStateRunning,
			wantNext:     true,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "a read that lists no checks over a check still running on the last step",
			arrange: func(_ *testing.T, f *syncFixture, args *internaljobs.RecoverOldChecksArgs) {
				f.forge.commitChecks = map[string]*forge.StatusCheckListing{supersededHeadSHA: listedStatusChecks(forge.ChecksNone)}
				args.Step = lastStep
			},
			wantState:    forge.StatusCheckStateUnknown,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "a reading with no listing of the head",
			arrange: func(_ *testing.T, f *syncFixture, _ *internaljobs.RecoverOldChecksArgs) {
				f.forge.commitChecks = map[string]*forge.StatusCheckListing{supersededHeadSHA: nil}
			},
			wantState:    forge.StatusCheckStateRunning,
			wantNext:     true,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "a head superseded longer ago than the cap",
			arrange: func(_ *testing.T, _ *syncFixture, args *internaljobs.RecoverOldChecksArgs) {
				args.SupersededAt = recoverAt.Add(-internaljobs.RecoverOldChecksCap - time.Minute)
			},
			wantState: forge.StatusCheckStateUnknown,
		},
		{
			name: "a read that failed",
			arrange: func(_ *testing.T, f *syncFixture, _ *internaljobs.RecoverOldChecksArgs) {
				f.forge.refuse = errors.New("github answered 502")
			},
			wantState:    forge.StatusCheckStateRunning,
			wantNext:     true,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "a read that failed on the last step",
			arrange: func(_ *testing.T, f *syncFixture, args *internaljobs.RecoverOldChecksArgs) {
				f.forge.refuse = errors.New("github answered 502")
				args.Step = lastStep
			},
			wantState:    forge.StatusCheckStateUnknown,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "the forge rate limiting the read",
			arrange: func(_ *testing.T, f *syncFixture, _ *internaljobs.RecoverOldChecksArgs) {
				f.forge.refuse = &forge.ThrottledError{RetryAt: recoverAt.Add(10 * time.Minute)}
			},
			wantState:    forge.StatusCheckStateRunning,
			wantSnooze:   10 * time.Minute,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "the forge rate limiting the read until a time already past",
			arrange: func(_ *testing.T, f *syncFixture, _ *internaljobs.RecoverOldChecksArgs) {
				f.forge.refuse = &forge.ThrottledError{RetryAt: recoverAt.Add(-time.Minute)}
			},
			wantState:    forge.StatusCheckStateRunning,
			wantSnooze:   minimumSnooze,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "a host its rate limit holds back",
			arrange: func(t *testing.T, f *syncFixture, _ *internaljobs.RecoverOldChecksArgs) {
				connection, err := f.connections.GetByHost(t.Context(), "github.com")
				require.NoError(t, err)
				until := recoverAt.Add(20 * time.Minute)
				require.NoError(t, f.connections.SetThrottledUntil(t.Context(), connection.ID, &until))
			},
			wantState:  forge.StatusCheckStateRunning,
			wantSnooze: 20 * time.Minute,
		},
		{
			name: "a host nobody connected",
			arrange: func(t *testing.T, f *syncFixture, _ *internaljobs.RecoverOldChecksArgs) {
				connection, err := f.connections.GetByHost(t.Context(), "github.com")
				require.NoError(t, err)
				require.NoError(t, f.connections.Delete(t.Context(), connection.ID))
			},
			wantState:  forge.StatusCheckStateRunning,
			wantSnooze: unreachableHostWait,
		},
		{
			name:         "a commit the forge no longer has",
			wantState:    forge.StatusCheckStateUnknown,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "a forge that cannot read a commit's checks",
			arrange: func(_ *testing.T, f *syncFixture, _ *internaljobs.RecoverOldChecksArgs) {
				f.forge.refuse = forge.ErrUnsupported
			},
			wantState:    forge.StatusCheckStateRunning,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "a refused credential",
			arrange: func(_ *testing.T, f *syncFixture, _ *internaljobs.RecoverOldChecksArgs) {
				f.forge.refuse = forge.ErrUnauthorized
			},
			wantState:    forge.StatusCheckStateRunning,
			wantRequests: []string{"checks:abc"},
		},
		{
			name: "a pull request that is gone",
			arrange: func(_ *testing.T, _ *syncFixture, args *internaljobs.RecoverOldChecksArgs) {
				args.PullRequestID = uuid.NewString()
			},
			wantState: forge.StatusCheckStateRunning,
		},
		{
			name: "a head the pull request moved back to",
			arrange: func(_ *testing.T, _ *syncFixture, args *internaljobs.RecoverOldChecksArgs) {
				args.HeadSHA = "def"
			},
			wantState: forge.StatusCheckStateRunning,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSyncFixture(t)
			pullRequestID := f.supersede(t)
			f.now = recoverAt
			args := internaljobs.RecoverOldChecksArgs{
				PullRequestID: pullRequestID,
				HeadSHA:       supersededHeadSHA,
				SupersededAt:  supersededAt,
			}
			if tt.arrange != nil {
				tt.arrange(t, f, &args)
			}
			queued := len(f.events.ListJobs())

			outcome, err := f.syncer.RecoverOldChecks(t.Context(), args)

			require.NoError(t, err)
			assert.Equal(t, tt.wantSnooze, outcome.Snooze)
			assert.Equal(t, tt.wantRequests, f.forge.requests)
			assert.Equal(t, tt.wantState, f.oldCheckState(t, pullRequestID))
			wantJobs := []river.JobArgs{}
			if tt.wantNext {
				next := args
				next.Step++
				wantJobs = append(wantJobs, next)
			}
			assert.Equal(t, wantJobs, f.events.ListJobs()[queued:])
		})
	}
}
