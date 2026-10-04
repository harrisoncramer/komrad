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

// PullRequest is one pull request opened from an agent's branch as the service stored it.
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
