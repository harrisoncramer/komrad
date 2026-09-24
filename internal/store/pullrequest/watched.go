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
	Feeders: []watch.Feeder{
		{
			Table:  "agent_pull_requests",
			Column: "pull_request_id",
		},
	},
}

// ListWatched returns the pull requests among keys that some agent has been seen with, or every one when keys is nil.
func (s *PullRequestStore) ListWatched(ctx context.Context, keys []string) ([]PullRequest, error) {
	rows, err := s.queries.ListWatchedPullRequests(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched pull requests: %w", err)
	}

	return xslice.Map(rows, fromRow), nil
}
