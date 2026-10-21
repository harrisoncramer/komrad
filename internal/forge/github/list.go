package github

import (
	"context"
	_ "embed"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
)

// maxPerPage is the most GitHub hands over in one page, above which it silently trims.
const maxPerPage = 100

//go:embed list.gql
var listOperation string

// listQuery is List with the fragment it spreads.
var listQuery = listOperation + pullRequestFragment

// List is one page of the repository's pull requests, every one from the named branch or
// the open ones when no branch is named, and where the next page starts.
func (f *Forge) List(ctx context.Context, params forge.ListParams) (*forge.Page, error) {
	owner, name, err := ownerAndName(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	variables := map[string]any{
		"owner":  owner,
		"name":   name,
		"first":  min(max(params.PerPage, 1), maxPerPage),
		"after":  graphql.CursorOf(params.Cursor),
		"states": []string{"OPEN"},
	}
	if params.Branch != "" {
		variables["branch"] = params.Branch
		variables["states"] = []string{"OPEN", "CLOSED", "MERGED"}
	}

	var data struct {
		Repository *struct {
			PullRequests connection `json:"pullRequests"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, listQuery, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, noRepository(variables)
	}

	return graphql.PageOf(&data.Repository.PullRequests, answer.RateLimit, func(node pullRequestNode) forge.PullRequest {
		return node.toPullRequest(false)
	}), nil
}
