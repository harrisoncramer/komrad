package pullrequests

import (
	pullrequeststore "komrad/internal/store/pullrequest"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// StateOf decides where a stored pull request stands: finished by its two stamps
// first, then draft, then a failing check, then a conflict with the base, then a change request,
// then a re-review asked of everyone who requested changes, then checks still running, then the
// forge's own word on whether it would merge it now, given passing checks, with an approval held
// at waiting until that word is clean and its checks have passed.
func StateOf(pullRequest pullrequeststore.PullRequest) pullrequestv1.PullRequestState {
	displayed := pullRequest.MergeReadiness.DisplayForHead(pullRequest.HeadSHA)
	checks, mergeability, decision := displayed.Checks.Value, displayed.Mergeability.Value, displayed.Decision.Value

	switch {
	case pullRequest.MergedAt != nil:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
	case pullRequest.ClosedAt != nil:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED
	case pullRequest.Draft:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT
	case checks == pullrequestv1.Checks_CHECKS_FAILING, checks == pullrequestv1.Checks_CHECKS_FAILING_RUNNING:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING
	case mergeability == pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING
	}

	switch {
	case decision == pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED
	case decision == pullrequestv1.ReviewDecision_REVIEW_DECISION_REREVIEW_REQUESTED:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED
	case checks == pullrequestv1.Checks_CHECKS_PENDING:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING
	case mergeability == pullrequestv1.Mergeability_MERGEABILITY_CLEAN && checks == pullrequestv1.Checks_CHECKS_PASSING:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED
	case decision == pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING
	}

	return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN
}
