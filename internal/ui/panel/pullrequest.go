package panel

import (
	"time"

	"komrad/internal/forge"

	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// PullRequestStanding is where the pull request opened from an agent's branch stands: the
// state, and the current one's number, address, title, head and merge readiness when there is one.
type PullRequestStanding struct {
	State  pullrequestv1.PullRequestState
	Number int32
	URL    string
	Title  string
	// HeadSHA is the commit the forge holds at the head of the current one.
	HeadSHA        string
	MergeReadiness MergeReadiness
	Forge          string
}

// PullRequest is one pull request opened from an agent's branch as the server stored it.
type PullRequest struct {
	ID             string
	Forge          string
	Number         int32
	URL            string
	Title          string
	Author         string
	Body           string
	State          pullrequestv1.PullRequestState
	MergeReadiness MergeReadiness
	OpenedAt       time.Time
	MergedAt       *time.Time
	ClosedAt       *time.Time
}

// Ref names the current one the way its forge writes it, such as #12 or !12.
func (s PullRequestStanding) Ref() string {
	return forge.Ref(s.Forge, int(s.Number))
}

// Noun is what the forge calls a pull request.
func (s PullRequestStanding) Noun() string {
	return forge.Noun(s.Forge)
}

// Abbrev is the short form its forge gives a pull request, such as pr or mr.
func (s PullRequestStanding) Abbrev() string {
	return forge.Abbrev(s.Forge)
}

// Ref names it the way its forge writes it, such as #12 or !12.
func (p PullRequest) Ref() string {
	return forge.Ref(p.Forge, int(p.Number))
}

// Noun is what its forge calls a pull request.
func (p PullRequest) Noun() string {
	return forge.Noun(p.Forge)
}

// MergeReadiness is how close a pull request is to merging, as komrad shows it for its head.
type MergeReadiness struct {
	Decision     pullrequestv1.ReviewDecision
	Checks       pullrequestv1.Checks
	Mergeability pullrequestv1.Mergeability
	// MergeabilityUnconfirmed says Mergeability is the forge's last answer for this head while it
	// works it out again.
	MergeabilityUnconfirmed bool
	// AutoMerge says the forge will merge it on its own once its checks and reviews pass.
	AutoMerge bool
	// AutoMergeable says the forge would let komrad turn auto-merge on for it now.
	AutoMergeable bool
}

// Words is the state word with what is still in the way beside it: a run in
// flight, a branch behind or conflicting, or a rule an approval is waiting on, a ready branch
// the forge is checking again, and failing checks with others still running.
func (p PullRequest) Words() string {
	state, checks, mergeability := p.State, p.MergeReadiness.Checks, p.MergeReadiness.Mergeability
	word := stateWord(state)
	switch state {
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT:
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING:
		if checks == pullrequestv1.Checks_CHECKS_FAILING_RUNNING {
			return word + ", others still running"
		}

		return word
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED:
		return word
	}

	switch {
	case state == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED && p.MergeReadiness.MergeabilityUnconfirmed:
		return word + ", forge rechecking"
	case mergeability == pullrequestv1.Mergeability_MERGEABILITY_CONFLICTING:
		return word + ", conflicts with the base"
	case mergeability == pullrequestv1.Mergeability_MERGEABILITY_BEHIND:
		return word + ", behind the base"
	case checks == pullrequestv1.Checks_CHECKS_PENDING:
		return word + ", checks running"
	case state == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING && mergeability == pullrequestv1.Mergeability_MERGEABILITY_BLOCKED:
		return word + ", blocked by a rule"
	case state == pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING && mergeability == pullrequestv1.Mergeability_MERGEABILITY_UNKNOWN:
		return word + ", forge still computing"
	}

	return word
}

// Describe says something of a task's pull request in its forge's words.
func (s PullRequestStanding) Describe(said string) string {
	if said == "" {
		return ""
	}

	return "its " + s.Noun() + " " + said
}

// stateWord is the one word the record uses for a state.
func stateWord(state pullrequestv1.PullRequestState) string {
	switch state {
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_OPEN:
		return "open"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_FAILING:
		return "checks failing"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CONFLICTING:
		return "conflicts with the base"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED_WAITING:
		return "approved"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_APPROVED:
		return "ready to merge"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHANGES_REQUESTED:
		return "changes requested"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_REREVIEW_REQUESTED:
		return "re-review requested"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CHECKS_RUNNING:
		return "checks running"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_DRAFT:
		return "draft"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_MERGED:
		return "merged"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_CLOSED:
		return "closed"
	case pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNSPECIFIED,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_UNREAD,
		pullrequestv1.PullRequestState_PULL_REQUEST_STATE_NONE:
		return ""
	}

	return "unknown"
}
