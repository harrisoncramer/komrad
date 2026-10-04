package github

import (
	"context"
	"fmt"
	"log/slog"

	"komrad/internal/forge"
	"komrad/internal/logging"
)

// ReadCommitChecks reads the checks on one commit, asking of the pull request which of them it
// requires, with the steps of each check run.
func (f *Forge) ReadCommitChecks(ctx context.Context, params forge.ReadCommitChecksParams) (*forge.Reading, error) {
	owner, name, err := ownerAndName(params.Repo.Path)
	if err != nil {
		return nil, err
	}

	variables := map[string]any{"owner": owner, "name": name, "sha": params.SHA, "pullRequest": params.ProviderID}
	operation := "query ReadCommitChecks($owner: String!, $name: String!, $sha: GitObjectID!, $pullRequest: ID!) {\n  repository(owner: $owner, name: $name) {\n    object(oid: $sha) { ... on Commit { " + commitChecksFields + " } }\n  }\n}"

	var data struct {
		Repository *struct {
			Object *commitChecksNode `json:"object"`
		} `json:"repository"`
	}
	answer, err := f.client.Query(ctx, operation, variables, &data)
	if err != nil {
		return nil, err
	}
	if err := failureOf(answer); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, noRepository(variables)
	}
	if data.Repository.Object == nil || data.Repository.Object.Oid != params.SHA {
		return nil, fmt.Errorf("%w: github has no commit %s in %s/%s", forge.ErrNotFound, params.SHA, owner, name)
	}

	reading := &forge.Reading{
		PullRequests: []forge.PullRequest{{
			ProviderID:     params.ProviderID,
			HeadSHA:        params.SHA,
			State:          "OPEN",
			MergeReadiness: &forge.MergeReadiness{Listing: data.Repository.Object.listing()},
		}},
		RateLimit: answer.RateLimit,
	}
	if err := f.readCheckSteps(ctx, reading); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "github did not list the steps of the check runs read, so the steps stored are kept until the next read", logging.Err(err)...)
	}

	return reading, nil
}
