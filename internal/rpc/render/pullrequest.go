package render

import (
	"weave/internal/forge"
	pullrequeststore "weave/internal/store/pullrequest"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
)

// PullRequestStateOf decides where a stored pull request stands: finished by its two stamps
// first, then draft, then the forge's review decision, else open.
func PullRequestStateOf(pullRequest pullrequeststore.PullRequest) pullrequestv1.PullRequestState {
	switch {
	case pullRequest.MergedAt != nil:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
	case pullRequest.ClosedAt != nil:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED
	case pullRequest.Draft:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT
	}

	if pullRequest.ReviewDecision == nil {
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN
	}

	switch *pullRequest.ReviewDecision {
	case forge.ReviewDecisionApproved:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED
	case forge.ReviewDecisionChangesRequested:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED
	case forge.ReviewDecisionNone, forge.ReviewDecisionPending:
	}

	return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN
}
