package pullrequests

import (
	"komrad/internal/forge"
	pullrequeststore "komrad/internal/store/pullrequest"
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
// forge's own word on whether it would merge it now, given passing checks, with an approval held
// at waiting until that word is clean and its checks have passed.
func StateOf(pullRequest pullrequeststore.PullRequest) State {
	displayed := pullRequest.MergeReadiness.DisplayForHead(pullRequest.HeadSHA)
	checks, mergeability, decision := displayed.Checks.Value, displayed.Mergeability.Value, displayed.Decision.Value

	switch {
	case pullRequest.MergedAt != nil:
		return StateMerged
	case pullRequest.ClosedAt != nil:
		return StateClosed
	case pullRequest.Draft:
		return StateDraft
	case checks == forge.ChecksFailing:
		return StateFailing
	case mergeability == forge.MergeabilityConflicting:
		return StateConflicting
	}

	switch {
	case decision == forge.ReviewDecisionChangesRequested:
		return StateChangesRequested
	case decision == forge.ReviewDecisionRereviewRequested:
		return StateRereviewRequested
	case checks == forge.ChecksPending:
		return StateChecksRunning
	case mergeability == forge.MergeabilityClean && checks == forge.ChecksPassing:
		return StateApproved
	case decision == forge.ReviewDecisionApproved:
		return StateApprovedWaiting
	}

	return StateOpen
}
