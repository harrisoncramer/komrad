package render

import (
	"testing"

	"komrad/internal/forge"
	"komrad/internal/pullrequests"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xptr"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"

	"github.com/stretchr/testify/assert"
)

// TestPullRequestStateOf covers each state a pull request can stand at.
func TestPullRequestStateOf(t *testing.T) {
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN, PullRequestStateOf(pullrequests.StateOpen))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT, PullRequestStateOf(pullrequests.StateDraft))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED, PullRequestStateOf(pullrequests.StateMerged))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED, PullRequestStateOf(pullrequests.StateClosed))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING, PullRequestStateOf(pullrequests.StateFailing))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING, PullRequestStateOf(pullrequests.StateConflicting))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED, PullRequestStateOf(pullrequests.StateChangesRequested))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED, PullRequestStateOf(pullrequests.StateRereviewRequested))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING, PullRequestStateOf(pullrequests.StateChecksRunning))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED, PullRequestStateOf(pullrequests.StateApproved))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING, PullRequestStateOf(pullrequests.StateApprovedWaiting))
}

// TestPullRequestDrawsAutoMerge covers a row the forge has not been asked about drawn as off, and
// the forge's word drawn as it was stored.
func TestPullRequestDrawsAutoMerge(t *testing.T) {
	tests := []struct {
		name              string
		pullRequest       pullrequeststore.PullRequest
		wantAutoMerge     bool
		wantAutoMergeable bool
	}{
		{
			name:        "a pull request not read yet",
			pullRequest: pullrequeststore.PullRequest{},
		},
		{
			name:              "a pull request the forge would queue",
			pullRequest:       pullrequeststore.PullRequest{AutoMerge: xptr.Ptr(false), AutoMergeable: true},
			wantAutoMergeable: true,
		},
		{
			name:          "a pull request the forge has queued",
			pullRequest:   pullrequeststore.PullRequest{AutoMerge: xptr.Ptr(true)},
			wantAutoMerge: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drawn := PullRequest(tt.pullRequest)

			assert.Equal(t, tt.wantAutoMerge, drawn.GetAutoMerge())
			assert.Equal(t, tt.wantAutoMergeable, drawn.GetAutoMergeable())
		})
	}
}

// TestMergeabilityOf covers each stored word and a row the forge has not computed.
func TestMergeabilityOf(t *testing.T) {
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN, MergeabilityOf(nil))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_CLEAN, MergeabilityOf(xptr.Ptr(forge.MergeabilityClean)))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_UNSTABLE, MergeabilityOf(xptr.Ptr(forge.MergeabilityUnstable)))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_BLOCKED, MergeabilityOf(xptr.Ptr(forge.MergeabilityBlocked)))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_BEHIND, MergeabilityOf(xptr.Ptr(forge.MergeabilityBehind)))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING, MergeabilityOf(xptr.Ptr(forge.MergeabilityConflicting)))
}

// TestChecksOf covers each stored word and a row the forge reports no checks on.
func TestChecksOf(t *testing.T) {
	assert.Equal(t, pullrequestv1.Checks_CHECKS_NONE, ChecksOf(nil))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_PENDING, ChecksOf(xptr.Ptr(forge.ChecksPending)))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_PASSING, ChecksOf(xptr.Ptr(forge.ChecksPassing)))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_FAILING, ChecksOf(xptr.Ptr(forge.ChecksFailing)))
}
