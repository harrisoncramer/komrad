package github

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"komrad/internal/forge"
	"komrad/internal/forge/graphql"
	"komrad/internal/git"
	"komrad/internal/logging"
)

// branchesPerQuery is how many branches one ReadBranches query asks about.
const branchesPerQuery = 50

// pullRequestsPerBranch is how many of a branch's newest pull requests ReadBranches reads.
const pullRequestsPerBranch = 5

// ReadBranches reads the newest pull requests opened from each branch, with their merge readiness,
// a batch of branches to a query by alias, keeping only those whose head is the repository itself.
func (f *Forge) ReadBranches(ctx context.Context, repo git.RemoteURL, branches []string) (*forge.Reading, error) {
	owner, name, err := ownerAndName(repo.Path)
	if err != nil {
		return nil, err
	}

	reading := &forge.Reading{}
	for batch := range slices.Chunk(branches, branchesPerQuery) {
		read, err := f.readBranches(ctx, owner, name, batch)
		if err != nil {
			return nil, err
		}
		reading.RateLimit = read.RateLimit
		reading.PullRequests = append(reading.PullRequests, read.PullRequests...)
	}

	return reading, nil
}

// readBranches is what one query answers about a batch of branches.
func (f *Forge) readBranches(ctx context.Context, owner, name string, branches []string) (*forge.Reading, error) {
	variables := map[string]any{"owner": owner, "name": name}
	var declared, fields strings.Builder
	for i, branch := range branches {
		variables["b"+strconv.Itoa(i)] = branch
		fmt.Fprintf(&declared, ", $b%d: String!", i)
		fmt.Fprintf(&fields, "\n    b%d: pullRequests(headRefName: $b%d, first: %d, orderBy: {field: CREATED_AT, direction: DESC}) { nodes { ...pullRequestFields %s } }", i, i, pullRequestsPerBranch, mergeReadinessFields)
	}
	operation := "query ReadBranches($owner: String!, $name: String!" + declared.String() + ") {\n  repository(owner: $owner, name: $name) {" + fields.String() + "\n  }\n}\n" + pullRequestFragment

	var data struct {
		Repository map[string]*graphql.Nodes[pullRequestNode] `json:"repository"`
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

	reading := &forge.Reading{
		PullRequests: graphql.OwnPullRequestsOf(data.Repository, len(branches), func(node pullRequestNode) forge.PullRequest {
			return node.toPullRequest(true)
		}),
		RateLimit: answer.RateLimit,
	}
	if err := f.readCheckListings(ctx, reading); err != nil {
		logging.From(ctx).LogAttrs(ctx, slog.LevelWarn, "github did not list the checks on the pull requests read, so they are read again next pass", logging.Err(err)...)
	}

	return reading, nil
}
