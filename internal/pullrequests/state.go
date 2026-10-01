package pullrequests

import (
	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
	"komrad/lib/xptr"
)

// State is where a pull request stands.
type State string

const (
	StateOpen              State = "open"
	StateDraft             State = "draft"
	StateMerged            State = "merged"
	StateClosed            State = "closed"
	StateFailing           State = "failing"
	StateConflicting       State = "conflicting"
	StateChangesRequested  State = "changes_requested"
	StateRereviewRequested State = "rereview_requested"
	StateChecksRunning     State = "checks_running"
	StateApproved          State = "approved"
	StateApprovedWaiting   State = "approved_waiting"
)

// StateOf decides where a stored pull request stands: finished by its two stamps
// first, then draft, then a failing check, then a conflict with the base, then a change request,
// then a re-review asked of everyone who requested changes, then checks still running, then the
// forge's own word on whether it would merge it now, with an approval held at waiting until that
// word is clean.
func StateOf(pullRequest pullrequeststore.PullRequest) State {
	checks := forge.ChecksNone
	if pullRequest.Checks != nil {
		checks = *pullRequest.Checks
	}
	mergeability, _ := ResolveMergeability(pullRequest)

	switch {
	case pullRequest.MergedAt != nil:
		return StateMerged
	case pullRequest.ClosedAt != nil:
		return StateClosed
	case pullRequest.Draft:
		return StateDraft
	case checks == forge.ChecksFailing:
		return StateFailing
	case mergeability != nil && *mergeability == forge.MergeabilityConflicting:
		return StateConflicting
	}

	decision := forge.ReviewDecisionNone
	if pullRequest.ReviewDecision != nil {
		decision = *pullRequest.ReviewDecision
	}

	switch {
	case decision == forge.ReviewDecisionChangesRequested:
		return StateChangesRequested
	case decision == forge.ReviewDecisionRereviewRequested:
		return StateRereviewRequested
	case checks == forge.ChecksPending:
		return StateChecksRunning
	case mergeability != nil && *mergeability == forge.MergeabilityClean:
		return StateApproved
	case decision == forge.ReviewDecisionApproved:
		return StateApprovedWaiting
	}

	return StateOpen
}

// ResolveMergeability returns the mergeability a pull request stands at and whether it is
// unconfirmed: the latest read, or while the forge is working it out again the last answer it
// gave for the same head.
func ResolveMergeability(pullRequest pullrequeststore.PullRequest) (*forge.Mergeability, bool) {
	if pullRequest.Mergeability != nil {
		return pullRequest.Mergeability, false
	}
	if pullRequest.LastKnownMergeability == "" || pullRequest.LastKnownMergeabilitySHA != pullRequest.HeadSHA {
		return nil, false
	}

	return xptr.Ptr(forge.Mergeability(pullRequest.LastKnownMergeability)), true
}
