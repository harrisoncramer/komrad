package render

import (
	"testing"

	"komrad/internal/forge"
	"komrad/internal/pullrequests"
	pullrequeststore "komrad/internal/store/pullrequest"
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

// TestPullRequestDrawsAuthor covers the forge login stored on the row reaching the wire unchanged,
// with no @ added, since the panel adds that.
func TestPullRequestDrawsAuthor(t *testing.T) {
	drawn := PullRequest(pullrequeststore.PullRequest{Author: "octocat"})

	assert.Equal(t, "octocat", drawn.GetAuthor())
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
			pullRequest:       pullrequeststore.PullRequest{MergeReadiness: forge.RememberedMergeReadiness{AutoMerge: &forge.AutoMerge{Allowed: true}}},
			wantAutoMergeable: true,
		},
		{
			name:          "a pull request the forge has queued",
			pullRequest:   pullrequeststore.PullRequest{MergeReadiness: forge.RememberedMergeReadiness{AutoMerge: &forge.AutoMerge{On: true}}},
			wantAutoMerge: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			drawn := PullRequest(tt.pullRequest)

			assert.Equal(t, tt.wantAutoMerge, drawn.GetMergeReadiness().GetAutoMerge())
			assert.Equal(t, tt.wantAutoMergeable, drawn.GetMergeReadiness().GetAutoMergeable())
		})
	}
}

// TestMergeabilityOf covers each stored word and a row the forge has not computed.
func TestMergeabilityOf(t *testing.T) {
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN, MergeabilityOf(forge.MergeabilityUnknown))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_CLEAN, MergeabilityOf(forge.MergeabilityClean))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_UNSTABLE, MergeabilityOf(forge.MergeabilityUnstable))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_BLOCKED, MergeabilityOf(forge.MergeabilityBlocked))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_BEHIND, MergeabilityOf(forge.MergeabilityBehind))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING, MergeabilityOf(forge.MergeabilityConflicting))
}

// TestChecksOf covers each stored word and a row the forge reports no checks on.
func TestChecksOf(t *testing.T) {
	assert.Equal(t, pullrequestv1.Checks_CHECKS_NONE, ChecksOf(forge.ChecksNone))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_PENDING, ChecksOf(forge.ChecksPending))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_PASSING, ChecksOf(forge.ChecksPassing))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_FAILING, ChecksOf(forge.ChecksFailing))
}
