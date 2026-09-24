package pullrequest

import (
	"context"
	"fmt"

	"weave/internal/watch"
	"weave/lib/xslice"
	watchv1 "weave/proto/gen/watch/v1"
)

// WatchedPullRequests declares the pull requests to the watch.
var WatchedPullRequests = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_PULL_REQUEST,
	Table:     "pull_requests",
	KeyColumn: "id",
}

// ListWatched returns whichever of keys hold a pull request on an agent's branch, or every one when keys is nil.
func (s *PullRequestStore) ListWatched(ctx context.Context, keys []string) ([]PullRequest, error) {
	if keys == nil {
		return s.List(ctx)
	}

	return s.ListByKey(ctx, keys)
}

// WatchedRepositories declares the repositories the sync reads to the watch.
var WatchedRepositories = watch.Resource{
	Enum:           watchv1.Resource_RESOURCE_REPOSITORY,
	Table:          "pull_request_syncs",
	KeyColumn:      "repository_url",
	IgnoredColumns: []string{"calls", "listed_at", "read_at", "sweep_cursor", "sweep_newest"},
}

// ListWatched returns the syncs of whichever repositories among keys have one, or of every repository when keys is nil.
func (s *PullRequestSyncStore) ListWatched(ctx context.Context, keys []string) ([]Sync, error) {
	rows, err := s.queries.ListWatchedRepositories(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched repositories: %w", err)
	}

	return xslice.Map(rows, fromSyncRow), nil
}
