package gitlab

import (
	"context"
	_ "embed"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
)

// maxPerPage is the most GitLab hands over in one page, above which it silently trims.
const maxPerPage = 100

//go:embed list.gql
var listOperation string

// listQuery is List with the fragment it spreads.
var listQuery = listOperation + mergeRequestFragment

// List is one page of the project's merge requests, every one from the named branch or the
// open ones when no branch is named, and where the next page starts.
func (f *Forge) List(ctx context.Context, params forge.ListParams) (*forge.Page, error) {
	path, err := projectPath(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	variables := map[string]any{
		"path":  path,
		"first": min(max(params.PerPage, 1), maxPerPage),
		"after": graphql.CursorOf(params.Cursor),
		"state": "opened",
	}
	if params.Branch != "" {
		variables["branches"] = []string{params.Branch}
		delete(variables, "state")
	}

	var data struct {
		Project *struct {
			MergeRequests connection `json:"mergeRequests"`
		} `json:"project"`
	}
	answer, err := f.client.Query(ctx, listQuery, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Project == nil {
		return nil, noProject(path)
	}

	return graphql.PageOf(&data.Project.MergeRequests, answer.RateLimit, func(node mergeRequestNode) forge.PullRequest {
		return node.toPullRequest(false)
	}), nil
}
