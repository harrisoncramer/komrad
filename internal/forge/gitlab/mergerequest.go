package gitlab

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/internal/git"
)

// mergeRequestFragment is every field of a merge request the record is built from.
const mergeRequestFragment = `fragment mergeRequestFields on MergeRequest {
  id iid title description author { username } webUrl state draft createdAt updatedAt mergedAt closedAt
  sourceBranch targetBranch diffHeadSha sourceProject { webUrl } targetProject { webUrl } diffRefs { baseSha }
}`

// mergeReadinessFields is what the decision, checks, mergeability and auto-merge of a merge request
// are folded from: its approvals, its reviewers' states, the status of the pipeline on its
// head, its detailed merge status, whether it is set to merge on its own and how it could be.
const mergeReadinessFields = `approvalsRequired approvalsLeft approvedBy { count } reviewers { nodes { mergeRequestInteraction { reviewState } } } headPipeline { id sha status mergeRequestEventType path startedAt finishedAt } detailedMergeStatus autoMergeEnabled availableAutoMergeStrategies`

const getQuery = `query Get($path: ID!, $iid: String!) {
  project(fullPath: $path) { mergeRequest(iid: $iid) { ...mergeRequestFields } }
}
` + mergeRequestFragment

// readMergeRequest is one merge request by its web address.
func (f *Forge) readMergeRequest(ctx context.Context, address string) (forge.PullRequest, error) {
	path, number, err := parseMergeRequestURL(address)
	if err != nil {
		return forge.PullRequest{}, err
	}

	var data struct {
		Project *struct {
			MergeRequest *mergeRequestNode `json:"mergeRequest"`
		} `json:"project"`
	}
	answer, err := f.client.Query(ctx, getQuery, map[string]any{"path": path, "iid": strconv.Itoa(number)}, &data)
	if err != nil {
		return forge.PullRequest{}, err
	}
	if err := failureOf(answer); err != nil {
		return forge.PullRequest{}, err
	}
	if data.Project == nil || data.Project.MergeRequest == nil {
		return forge.PullRequest{}, noMergeRequest(number, path)
	}

	return data.Project.MergeRequest.toPullRequest(false), nil
}

// projectPath is the full path a project is addressed by.
func projectPath(path string) (string, error) {
	if path == "" || !strings.Contains(path, "/") || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return "", fmt.Errorf("%w: %q is not group/project", forge.ErrNotRepository, path)
	}

	return path, nil
}

// parseMergeRequestURL reads the project path and the number out of
// https://host/group/project/-/merge_requests/N.
func parseMergeRequestURL(address string) (path string, number int, err error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Hostname() == "" {
		return "", 0, fmt.Errorf("%w: %q is not a gitlab merge request", forge.ErrNotPullRequest, address)
	}

	before, after, ok := strings.Cut(strings.Trim(parsed.Path, "/"), "/-/merge_requests/")
	if !ok || !strings.Contains(before, "/") {
		return "", 0, fmt.Errorf("%w: %q is not a gitlab merge request", forge.ErrNotPullRequest, address)
	}

	number, err = strconv.Atoi(strings.Split(after, "/")[0])
	if err != nil || number <= 0 {
		return "", 0, fmt.Errorf("%w: %q is not a gitlab merge request", forge.ErrNotPullRequest, address)
	}

	return before, number, nil
}

// connection is one page of a merge request connection as GitLab answers it.
type connection = graphql.Connection[mergeRequestNode]

// mergeRequestNode is the slice of GitLab's merge request node the record is built from.
type mergeRequestNode struct {
	mergeReadinessNode
	ID            string       `json:"id"`
	IID           string       `json:"iid"`
	Title         string       `json:"title"`
	Description   string       `json:"description"`
	Author        *userNode    `json:"author"`
	WebURL        string       `json:"webUrl"`
	State         string       `json:"state"`
	Draft         bool         `json:"draft"`
	CreatedAt     time.Time    `json:"createdAt"`
	UpdatedAt     time.Time    `json:"updatedAt"`
	MergedAt      *time.Time   `json:"mergedAt"`
	ClosedAt      *time.Time   `json:"closedAt"`
	SourceBranch  string       `json:"sourceBranch"`
	TargetBranch  string       `json:"targetBranch"`
	SourceProject *projectNode `json:"sourceProject"`
	TargetProject *projectNode `json:"targetProject"`
	DiffRefs      *struct {
		BaseSha string `json:"baseSha"`
	} `json:"diffRefs"`
}

type userNode struct {
	Username string `json:"username"`
}

func (n *userNode) username() string {
	if n == nil {
		return ""
	}

	return n.Username
}

type projectNode struct {
	WebURL string `json:"webUrl"`
}

// remote is the project the node names, zero for one GitLab no longer has.
func (n *projectNode) remote() git.RemoteURL {
	if n == nil {
		return git.RemoteURL{}
	}

	return graphql.RemoteOf(n.WebURL)
}

// mergeReadinessNode is what GitLab says about a merge request's reviews and the pipeline on its
// head.
type mergeReadinessNode struct {
	DiffHeadSha       string `json:"diffHeadSha"`
	ApprovalsRequired int    `json:"approvalsRequired"`
	ApprovalsLeft     int    `json:"approvalsLeft"`
	ApprovedBy        struct {
		Count int `json:"count"`
	} `json:"approvedBy"`
	Reviewers struct {
		Nodes []struct {
			MergeRequestInteraction *struct {
				ReviewState string `json:"reviewState"`
			} `json:"mergeRequestInteraction"`
		} `json:"nodes"`
	} `json:"reviewers"`
	HeadPipeline                 *pipelineNode `json:"headPipeline"`
	DetailedMergeStatus          string        `json:"detailedMergeStatus"`
	AutoMergeEnabled             bool          `json:"autoMergeEnabled"`
	AvailableAutoMergeStrategies []string      `json:"availableAutoMergeStrategies"`
}

// toPullRequest is the node as the record, carrying its merge readiness when the query asked
// for it.
func (n mergeRequestNode) toPullRequest(withMergeReadiness bool) forge.PullRequest {
	baseSHA := ""
	if n.DiffRefs != nil {
		baseSHA = n.DiffRefs.BaseSha
	}

	pullRequest := forge.PullRequest{
		Forge:      forge.KindGitLab,
		ProviderID: strings.TrimPrefix(n.ID, "gid://gitlab/MergeRequest/"),
		URL:        n.WebURL,
		Repository: n.TargetProject.remote(),
		Head:       n.SourceProject.remote(),
		Number:     numberOf(n.IID),
		Title:      n.Title,
		Author:     n.Author.username(),
		Body:       n.Description,
		HeadBranch: n.SourceBranch,
		HeadSHA:    n.DiffHeadSha,
		BaseBranch: n.TargetBranch,
		BaseSHA:    baseSHA,
		Draft:      n.Draft,
		State:      stateOf(n.State),
		CreatedAt:  n.CreatedAt,
		UpdatedAt:  n.UpdatedAt,
		MergedAt:   n.MergedAt,
		ClosedAt:   n.closedAt(),
	}
	if withMergeReadiness {
		pullRequest.MergeReadiness = &forge.MergeReadiness{
			Decision:     n.decision(),
			Listing:      n.listing(),
			Mergeability: n.mergeability(),
			AutoMerge:    n.autoMerge(),
		}
	}

	return pullRequest
}

// numberOf is the iid as a number.
func numberOf(iid string) int {
	number, err := strconv.Atoi(iid)
	if err != nil {
		return 0
	}

	return number
}

// closedAt is when the merge request finished, which GitLab stamps only on a close and komrad
// records on a merge as well.
func (n mergeRequestNode) closedAt() *time.Time {
	if n.ClosedAt != nil {
		return n.ClosedAt
	}

	return n.MergedAt
}

// stateOf is GitLab's state in the words GitHub's enum uses.
func stateOf(state string) string {
	if state == "opened" {
		return "OPEN"
	}

	return strings.ToUpper(state)
}
