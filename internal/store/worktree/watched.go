package worktree

import (
	"context"
	"fmt"

	"komrade/db/models"
	"komrade/internal/watch"
	"komrade/lib/xslice"
	watchv1 "komrade/proto/gen/watch/v1"
)

// WatchedWorktrees declares the worktree directories to the watch.
var WatchedWorktrees = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_WORKTREE,
	Table:     "worktrees",
	KeyColumn: "dir",
	Feeders: []watch.Feeder{
		{
			Table: "agents",
			Lookup: &watch.Lookup{
				ParentColumn: "name",
				ChildColumn:  "id",
				OnChangeOf:   "deleting_at",
			},
		},
	},
}

// ListWatched returns whichever of keys are recorded directories whose agent is not being deleted, or every one when keys is nil.
func (s *WorktreeStore) ListWatched(ctx context.Context, keys []string) ([]Worktree, error) {
	rows, err := s.queries.ListWatchedWorktrees(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees: %w", err)
	}

	return xslice.Map(rows, func(row *models.ListWatchedWorktreesRow) Worktree {
		return worktreeFrom((*models.ListWorktreesRow)(row))
	}), nil
}
