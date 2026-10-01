package panel

import (
	"time"

	pullrequestv1 "komrad/proto/gen/pullrequest/v1"
)

// PullRequestStanding is where the pull request opened from an agent's branch stands: the
// state, the checks on its head, its mergeability, and the current one's number, address, title,
// head and auto-merge when there is one.
type PullRequestStanding struct {
	State        pullrequestv1.PullRequestState
	Checks       pullrequestv1.Checks
	Mergeability pullrequestv1.Mergeability
	Number       int32
	URL          string
	Title        string
	// HeadSHA is the commit the forge holds at the head of the current one.
	HeadSHA string
	// AutoMerge says the forge will merge the current one on its own once its checks and reviews
	// pass.
	AutoMerge bool
	// AutoMergeable says the forge would let komrad turn auto-merge on for the current one now.
	AutoMergeable bool
}

// PullRequest is one pull request opened from an agent's branch as the service stored it.
type PullRequest struct {
	Number       int32
	URL          string
	Title        string
	State        pullrequestv1.PullRequestState
	Checks       pullrequestv1.Checks
	Mergeability pullrequestv1.Mergeability
	// MergeabilityUnconfirmed says Mergeability is the forge's last answer for this head while it
	// works it out again.
	MergeabilityUnconfirmed bool
	OpenedAt                time.Time
	MergedAt                *time.Time
	ClosedAt                *time.Time
}
