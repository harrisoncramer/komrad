package render

import (
	"komrade/internal/forge"
	pullrequeststore "komrade/internal/store/pullrequest"
	"komrade/lib/dates"
	pullrequestv1 "komrade/proto/gen/pullrequest/v1"
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
		HeadBranch:        stored.HeadBranch,
		HeadSha:           stored.HeadSHA,
		BaseBranch:        stored.BaseBranch,
		BaseSha:           stored.BaseSHA,
		Draft:             stored.Draft,
		State:             PullRequestStateOf(stored),
		OpenedAt:          dates.Stamp(stored.OpenedAt),
		ChangedAt:         dates.Stamp(stored.ChangedAt),
		MergedAt:          dates.StampPtr(stored.MergedAt),
		ClosedAt:          dates.StampPtr(stored.ClosedAt),
		ReviewDecision:    ReviewDecisionOf(stored.ReviewDecision),
		Checks:            ChecksOf(stored.Checks),
		Mergeability:      MergeabilityOf(stored.Mergeability),
	}
}

// PullRequestStateOf decides where a stored pull request stands: finished by its two stamps
// first, then draft, then a failing check, then a conflict with the base, then a change request,
// then the forge's own word on whether it would merge it now, with an approval held at waiting
// until that word is clean.
func PullRequestStateOf(pullRequest pullrequeststore.PullRequest) pullrequestv1.PullRequestState {
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
	case pullRequest.Mergeability != nil && *pullRequest.Mergeability == forge.MergeabilityConflicting:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING
	}

	decision := forge.ReviewDecisionNone
	if pullRequest.ReviewDecision != nil {
		decision = *pullRequest.ReviewDecision
	}

	switch {
	case decision == forge.ReviewDecisionChangesRequested:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED
	case pullRequest.Mergeability != nil && *pullRequest.Mergeability == forge.MergeabilityClean:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED
	case decision == forge.ReviewDecisionApproved:
		return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING
	}

	return pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN
}

// ReviewDecisionOf is the stored review decision as the wire carries it, NONE for a row the
// forge has said nothing about.
func ReviewDecisionOf(decision *forge.ReviewDecision) pullrequestv1.ReviewDecision {
	if decision == nil {
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE
	}

	switch *decision {
	case forge.ReviewDecisionPending:
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_PENDING
	case forge.ReviewDecisionApproved:
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_APPROVED
	case forge.ReviewDecisionChangesRequested:
		return pullrequestv1.ReviewDecision_REVIEW_DECISION_CHANGES_REQUESTED
	case forge.ReviewDecisionNone:
	}

	return pullrequestv1.ReviewDecision_REVIEW_DECISION_NONE
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

// Repository draws where the sync is for one repository as the wire carries it.
func Repository(sync pullrequeststore.Sync) *pullrequestv1.Repository {
	return &pullrequestv1.Repository{
		Url:  sync.RepositoryURL,
		Read: sync.IsSwept(),
	}
}
