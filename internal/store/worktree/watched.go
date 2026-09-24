package worktree

import (
	"context"
	"fmt"

	"weave/internal/watch"
	watchv1 "weave/proto/gen/watch/v1"
)

// WatchedWorktrees declares the worktree directories to the watch.
var WatchedWorktrees = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_WORKTREE,
	Table:     "worktrees",
	KeyColumn: "dir",
}

// ListWatched returns whichever of keys are still recorded directories, or every one when keys is nil.
func (s *WorktreeStore) ListWatched(ctx context.Context, keys []string) ([]Worktree, error) {
	rows, err := s.queries.ListWatchedWorktrees(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	return worktreesFrom(rows), nil
}
