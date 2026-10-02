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
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN, pullRequestStateOf(pullrequests.StateOpen))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT, pullRequestStateOf(pullrequests.StateDraft))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED, pullRequestStateOf(pullrequests.StateMerged))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED, pullRequestStateOf(pullrequests.StateClosed))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING, pullRequestStateOf(pullrequests.StateFailing))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING, pullRequestStateOf(pullrequests.StateConflicting))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED, pullRequestStateOf(pullrequests.StateChangesRequested))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED, pullRequestStateOf(pullrequests.StateRereviewRequested))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING, pullRequestStateOf(pullrequests.StateChecksRunning))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED, pullRequestStateOf(pullrequests.StateApproved))
	assert.Equal(t, pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING, pullRequestStateOf(pullrequests.StateApprovedWaiting))
}

// TestPullRequestDrawsAuthorAndBody covers the forge login and description stored on the row
// reaching the wire unchanged, with no @ added to the login, since the panel adds that.
func TestPullRequestDrawsAuthorAndBody(t *testing.T) {
	drawn := PullRequest(pullrequeststore.PullRequest{Author: "octocat", Body: "Why it changed.\n\nMore."})

	assert.Equal(t, "octocat", drawn.GetAuthor())
	assert.Equal(t, "Why it changed.\n\nMore.", drawn.GetBody())
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
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN, mergeabilityOf(forge.MergeabilityUnknown))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_CLEAN, mergeabilityOf(forge.MergeabilityClean))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_UNSTABLE, mergeabilityOf(forge.MergeabilityUnstable))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_BLOCKED, mergeabilityOf(forge.MergeabilityBlocked))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_BEHIND, mergeabilityOf(forge.MergeabilityBehind))
	assert.Equal(t, pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING, mergeabilityOf(forge.MergeabilityConflicting))
}

// TestChecksOf covers each stored word and a row the forge reports no checks on.
func TestChecksOf(t *testing.T) {
	assert.Equal(t, pullrequestv1.Checks_CHECKS_NONE, checksOf(forge.ChecksNone))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_PENDING, checksOf(forge.ChecksPending))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_PASSING, checksOf(forge.ChecksPassing))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_FAILING, checksOf(forge.ChecksFailing))
	assert.Equal(t, pullrequestv1.Checks_CHECKS_FAILING_RUNNING, checksOf(forge.ChecksFailingRunning))
}
