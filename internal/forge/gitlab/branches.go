package gitlab

import (
	"context"
	_ "embed"
	"errors"
	"slices"

	"komrad/internal/fail"
	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/internal/git"
	"komrad/lib/xslice"
)

// maxSourceBranches is the most branches GitLab filters merge requests by in one query.
const maxSourceBranches = 1000

// mergeRequestsPerBranch is how many of a branch's newest merge requests ReadBranches reads.
const mergeRequestsPerBranch = 5

//go:embed readbranches.gql
var readBranchesOperation string

// readBranchesQuery is ReadBranches with the fragments it spreads.
var readBranchesQuery = readBranchesOperation + mergeRequestFragment + mergeReadinessFragment

// errTooComplex is GitLab refusing a query for its complexity.
var errTooComplex = fail.Exhausted("gitlab refused the query for its complexity")

// ReadBranches reads the newest merge requests opened from each branch, with their merge readiness,
// paging until every branch has its newest five and keeping only those whose source is the project
// itself.
func (f *Forge) ReadBranches(ctx context.Context, repo git.RemoteURL, branches []string) (*forge.Reading, error) {
	path, err := projectPath(repo.Path)
	if err != nil {
		return nil, err
	}

	reading := &forge.Reading{}
	first := maxPerPage
	for batch := range slices.Chunk(branches, maxSourceBranches) {
		seen := make(map[string]int, len(batch))
		after := ""
		for len(batch) > 0 {
			page, err := f.readBranchesPage(ctx, readBranchesPageParams{
				path:     path,
				branches: batch,
				first:    first,
				after:    after,
			})
			if errors.Is(err, errTooComplex) && first > 1 {
				first /= 2

				continue
			}
			if err != nil {
				return nil, err
			}

			reading.RateLimit = page.RateLimit
			for _, node := range page.MergeRequests.Nodes {
				if seen[node.SourceBranch] == mergeRequestsPerBranch {
					continue
				}
				seen[node.SourceBranch]++

				pullRequest := node.toPullRequest(true)
				if pullRequest.IsFromItsOwnRepository() {
					reading.PullRequests = append(reading.PullRequests, pullRequest)
				}
			}

			if !page.MergeRequests.PageInfo.HasNextPage {
				break
			}
			after = page.MergeRequests.PageInfo.EndCursor
			batch = xslice.Filter(batch, func(branch string) bool {
				return seen[branch] < mergeRequestsPerBranch
			})
		}
	}

	return reading, nil
}

// mergeRequestPage is one page of the merge requests from a set of branches and the rate limit after it.
type mergeRequestPage struct {
	MergeRequests connection
	RateLimit     forge.RateLimit
}

// readBranchesPageParams is the project, the branches and which page of their merge requests to read.
type readBranchesPageParams struct {
	path     string
	branches []string
	first    int
	after    string
}

// readBranchesPage is one page of the merge requests opened from any of the branches, newest first.
func (f *Forge) readBranchesPage(ctx context.Context, params readBranchesPageParams) (*mergeRequestPage, error) {
	variables := map[string]any{
		"path":     params.path,
		"branches": params.branches,
		"first":    params.first,
		"after":    graphql.CursorOf(params.after),
	}

	var data struct {
		Project *struct {
			MergeRequests connection `json:"mergeRequests"`
		} `json:"project"`
	}
	answer, err := f.client.Query(ctx, readBranchesQuery, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Project == nil {
		return nil, noProject(params.path)
	}

	return &mergeRequestPage{MergeRequests: data.Project.MergeRequests, RateLimit: answer.RateLimit}, nil
}
