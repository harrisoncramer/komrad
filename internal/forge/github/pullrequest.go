package github

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/internal/git"
)

// pullRequestFragment is every field of a pull request the record is built from.
const pullRequestFragment = `fragment pullRequestFields on PullRequest {
  id number title body author { login } url state isDraft createdAt updatedAt mergedAt closedAt
  headRefName headRefOid baseRefName baseRefOid headRepository { url } baseRepository { url }
}`

// mergeReadinessFields is what the reviews, mergeability and auto-merge of a pull request are read from.
const mergeReadinessFields = `reviewDecision latestOpinionatedReviews(first: 50, writersOnly: true) { nodes { state author { ... on User { login } } } } reviewRequests(first: 50) { nodes { requestedReviewer { ... on User { login } } } } mergeStateStatus autoMergeRequest { enabledAt } viewerCanEnableAutoMerge`

// ownerAndName splits a repository path, which on GitHub is always two segments.
func ownerAndName(path string) (owner, name string, err error) {
	owner, name, ok := strings.Cut(path, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("%w: %q is not owner/name", forge.ErrNotRepository, path)
	}

	return owner, name, nil
}

// parsePullURL reads the owner, name and number out of https://host/owner/name/pull/N.
func parsePullURL(address string) (owner, name string, number int, err error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return "", "", 0, fmt.Errorf("%w: %q is not a github pull request", forge.ErrNotPullRequest, address)
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(segments) < 4 || segments[2] != "pull" {
		return "", "", 0, fmt.Errorf("%w: %q is not a github pull request", forge.ErrNotPullRequest, address)
	}

	number, err = strconv.Atoi(segments[3])
	if err != nil || number <= 0 {
		return "", "", 0, fmt.Errorf("%w: %q is not a github pull request", forge.ErrNotPullRequest, address)
	}

	return segments[0], segments[1], number, nil
}

// connection is one page of a pull request connection as GitHub answers it.
type connection = graphql.Connection[pullRequestNode]

// pullRequestNode is the slice of GitHub's pull request node the record is built from.
type pullRequestNode struct {
	mergeReadinessNode
	ID             string          `json:"id"`
	Number         int             `json:"number"`
	Title          string          `json:"title"`
	Body           string          `json:"body"`
	Author         *actorNode      `json:"author"`
	URL            string          `json:"url"`
	State          string          `json:"state"`
	IsDraft        bool            `json:"isDraft"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	MergedAt       *time.Time      `json:"mergedAt"`
	ClosedAt       *time.Time      `json:"closedAt"`
	HeadRefName    string          `json:"headRefName"`
	BaseRefName    string          `json:"baseRefName"`
	BaseRefOid     string          `json:"baseRefOid"`
	HeadRepository *repositoryNode `json:"headRepository"`
	BaseRepository *repositoryNode `json:"baseRepository"`
}

type repositoryNode struct {
	URL string `json:"url"`
}

// mergeReadinessNode is what GitHub says about a pull request's reviews and its merge state.
type mergeReadinessNode struct {
	HeadRefOid               string  `json:"headRefOid"`
	ReviewDecision           *string `json:"reviewDecision"`
	LatestOpinionatedReviews struct {
		Nodes []opinionNode `json:"nodes"`
	} `json:"latestOpinionatedReviews"`
	ReviewRequests struct {
		Nodes []struct {
			RequestedReviewer *actorNode `json:"requestedReviewer"`
		} `json:"nodes"`
	} `json:"reviewRequests"`
	MergeStateStatus         string    `json:"mergeStateStatus"`
	AutoMergeRequest         *struct{} `json:"autoMergeRequest"`
	ViewerCanEnableAutoMerge bool      `json:"viewerCanEnableAutoMerge"`
}

// remote is the repository the node names, zero for one GitHub no longer has.
func (n *repositoryNode) remote() git.RemoteURL {
	if n == nil {
		return git.RemoteURL{}
	}

	return graphql.RemoteOf(n.URL)
}

// toPullRequest is the node as the record, carrying its merge readiness when the query asked
// for it.
func (n pullRequestNode) toPullRequest(withMergeReadiness bool) forge.PullRequest {
	pullRequest := forge.PullRequest{
		Forge:      forge.KindGitHub,
		ProviderID: n.ID,
		URL:        n.URL,
		Repository: n.BaseRepository.remote(),
		Head:       n.HeadRepository.remote(),
		Number:     n.Number,
		Title:      n.Title,
		Author:     n.Author.login(),
		Body:       n.Body,
		HeadBranch: n.HeadRefName,
		HeadSHA:    n.HeadRefOid,
		BaseBranch: n.BaseRefName,
		BaseSHA:    n.BaseRefOid,
		Draft:      n.IsDraft,
		State:      n.State,
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
		MergedAt:   n.MergedAt,
		ClosedAt:   n.ClosedAt,
	}
	if withMergeReadiness {
		pullRequest.MergeReadiness = &forge.MergeReadiness{
			Decision:     n.decision(),
			Mergeability: mergeabilityOf(n.MergeStateStatus),
			AutoMerge:    n.autoMerge(),
		}
	}

	return pullRequest
}
