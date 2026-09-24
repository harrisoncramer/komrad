package pullrequest

import (
	"context"

	"weave/internal/watch"
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
