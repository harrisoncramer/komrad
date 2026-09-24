package panel

import (
	"time"

	pullrequestv1 "komrade/proto/gen/pullrequest/v1"
)

// PullRequestStanding is where the pull request opened from an agent's branch stands: the
// state, the checks on its head, its mergeability, and the current one's number, address and
// title when there is one.
type PullRequestStanding struct {
	State        pullrequestv1.PullRequestState
	Checks       pullrequestv1.Checks
	Mergeability pullrequestv1.Mergeability
	Number       int32
	URL          string
	Title        string
}

// PullRequest is one pull request opened from an agent's branch as the service stored it.
type PullRequest struct {
	Number       int32
	URL          string
	Title        string
	State        pullrequestv1.PullRequestState
	Checks       pullrequestv1.Checks
	Mergeability pullrequestv1.Mergeability
	OpenedAt     time.Time
	MergedAt     *time.Time
	ClosedAt     *time.Time
}
