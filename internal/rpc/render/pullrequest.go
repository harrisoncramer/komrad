package render

import (
	"weave/internal/forge"
	pullrequeststore "weave/internal/store/pullrequest"
	pullrequestv1 "weave/proto/gen/pullrequest/v1"
)

// PullRequestStateOf decides where a stored pull request stands: finished by its two stamps
// first, then draft, then a failing check, then the forge's review decision, with an approval
// held at waiting until the forge says it would merge it.
func PullRequestStateOf(pullRequest pullrequeststore.Summary) pullrequestv1.PullRequestState {
	checks := forge.ChecksNone
	if pullRequest.Checks != nil {
		checks = *pullRequest.Checks
	}

	switch {
	case pullRequest.MergedAt != nil:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
	case pullRequest.ClosedAt != nil:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED
	case pullRequest.Draft:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT
	case checks == forge.ChecksFailing:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING
	}

	if pullRequest.ReviewDecision == nil {
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN
	}

	switch *pullRequest.ReviewDecision {
	case forge.ReviewDecisionApproved:
		if pullRequest.Mergeability != nil && *pullRequest.Mergeability == forge.MergeabilityClean {
			return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED
		}

		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING
	case forge.ReviewDecisionChangesRequested:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED
	case forge.ReviewDecisionNone, forge.ReviewDecisionPending:
	}

	return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN
}

// ChecksOf is the stored checks as the wire carries them, NONE for a row the forge reports no
// checks on.
func ChecksOf(checks *forge.Checks) pullrequestv1.Checks {
	if checks == nil {
		return pullrequestv1.Checks_CHECKS_NONE
	}

	switch *checks {
	case forge.ChecksPending:
		return pullrequestv1.Checks_CHECKS_PENDING
	case forge.ChecksPassing:
		return pullrequestv1.Checks_CHECKS_PASSING
	case forge.ChecksFailing:
		return pullrequestv1.Checks_CHECKS_FAILING
	case forge.ChecksNone:
	}

	return pullrequestv1.Checks_CHECKS_NONE
}

// MergeabilityOf is the stored mergeability as the wire carries it, UNKNOWN for a row the forge
// has not computed.
func MergeabilityOf(mergeability *forge.Mergeability) pullrequestv1.Mergeability {
	if mergeability == nil {
		return pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN
	}

	switch *mergeability {
	case forge.MergeabilityClean:
		return pullrequestv1.Mergeability_MERGEABILITY_CLEAN
	case forge.MergeabilityUnstable:
		return pullrequestv1.Mergeability_MERGEABILITY_UNSTABLE
	case forge.MergeabilityBlocked:
		return pullrequestv1.Mergeability_MERGEABILITY_BLOCKED
	case forge.MergeabilityBehind:
		return pullrequestv1.Mergeability_MERGEABILITY_BEHIND
	case forge.MergeabilityConflicting:
		return pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING
	case forge.MergeabilityUnknown:
	}

	return pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN
}
