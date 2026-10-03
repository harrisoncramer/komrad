package render

import (
	"komrad/internal/forge"
	"komrad/internal/pullrequests"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/dates"
	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// PullRequest draws one stored pull request as the wire carries it.
func PullRequest(stored pullrequeststore.PullRequest) *pullrequestv1.PullRequest {
	return &pullrequestv1.PullRequest{
		Id:                stored.ID,
		Forge:             stored.Forge,
		ProviderId:        stored.ProviderID,
		Url:               stored.URL,
		RepositoryUrl:     stored.RepositoryURL,
		HeadRepositoryUrl: stored.HeadRepositoryURL,
		Number:            int32(stored.Number),
		Title:             stored.Title,
		Author:            stored.Author,
		Body:              stored.Body,
		HeadBranch:        stored.HeadBranch,
		HeadSha:           stored.HeadSHA,
		BaseBranch:        stored.BaseBranch,
		BaseSha:           stored.BaseSHA,
		Draft:             stored.Draft,
		State:             pullRequestStateOf(pullrequests.StateOf(stored)),
		OpenedAt:          dates.Stamp(stored.OpenedAt),
		ChangedAt:         dates.Stamp(stored.ChangedAt),
		MergedAt:          dates.StampPtr(stored.MergedAt),
		ClosedAt:          dates.StampPtr(stored.ClosedAt),
		MergeReadiness:    mergeReadiness(stored.MergeReadiness.DisplayForHead(stored.HeadSHA)),
	}
}

// mergeReadiness draws what komrad shows of a pull request's merge readiness as the wire carries it.
func mergeReadiness(displayed forge.DisplayedMergeReadiness) *pullrequestv1.MergeReadiness {
	return &pullrequestv1.MergeReadiness{
		Decision:                reviewDecisionOf(displayed.Decision.Value),
		Checks:                  checksOf(displayed.Checks.Value),
		Mergeability:            mergeabilityOf(displayed.Mergeability.Value),
		MergeabilityUnconfirmed: displayed.Mergeability.IsUnconfirmed,
		AutoMerge:               displayed.AutoMerge.On,
		AutoMergeable:           displayed.AutoMerge.Allowed,
	}
}

// pullRequestStateOf is a pull request's state as the wire carries it.
func pullRequestStateOf(state pullrequests.State) pullrequestv1.PullRequestState {
	switch state {
	case pullrequests.StateMerged:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED
	case pullrequests.StateClosed:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED
	case pullrequests.StateDraft:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT
	case pullrequests.StateFailing:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING
	case pullrequests.StateConflicting:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING
	case pullrequests.StateChangesRequested:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED
	case pullrequests.StateRereviewRequested:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED
	case pullrequests.StateChecksRunning:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING
	case pullrequests.StateApproved:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED
	case pullrequests.StateApprovedWaiting:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING
	case pullrequests.StateOpen:
	}

	return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN
}

// reviewDecisionOf is the review decision as the wire carries it.
func reviewDecisionOf(decision forge.ReviewDecision) pullrequestv1.ReviewDecision {
	switch decision {
	case forge.ReviewDecisionPending:
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING
	case forge.ReviewDecisionApproved:
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED
	case forge.ReviewDecisionChangesRequested:
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED
	case forge.ReviewDecisionRereviewRequested:
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_REREVIEW_REQUESTED
	case forge.ReviewDecisionNone:
	}

	return pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE
}

// checksOf is the checks as the wire carries them.
func checksOf(checks forge.Checks) pullrequestv1.Checks {
	switch checks {
	case forge.ChecksPending:
		return pullrequestv1.Checks_CHECKS_PENDING
	case forge.ChecksPassing:
		return pullrequestv1.Checks_CHECKS_PASSING
	case forge.ChecksFailing:
		return pullrequestv1.Checks_CHECKS_FAILING
	case forge.ChecksFailingRunning:
		return pullrequestv1.Checks_CHECKS_FAILING_RUNNING
	case forge.ChecksNone:
	}

	return pullrequestv1.Checks_CHECKS_NONE
}

// mergeabilityOf is the mergeability as the wire carries it.
func mergeabilityOf(mergeability forge.Mergeability) pullrequestv1.Mergeability {
	switch mergeability {
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

// Repository draws a repository the sync has read as the wire carries it.
func Repository(sync pullrequeststore.Sync) *pullrequestv1.Repository {
	return &pullrequestv1.Repository{Url: sync.RepositoryURL}
}
