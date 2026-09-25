package row

import (
	"context"
	"fmt"

	"komrad/internal/store"
	"komrad/internal/watch"
	watchv1 "komrad/proto/gen/watch/v1"
)

// WatchedAgents declares the agents to the watch.
var WatchedAgents = watch.Resource{
	Enum:           watchv1.Resource_RESOURCE_AGENT,
	Table:          "agents",
	KeyColumn:      "id",
	IgnoredColumns: []string{"state_seen_at", "updated_at"},
	Feeders: []watch.Feeder{
		{
			Table:  "agent_runs",
			Column: "agent_id",
		},
	},
}

// ListWatched returns the rows of the agents among keys that are not being deleted, or of every one when keys is nil.
func (s *RowStore) ListWatched(ctx context.Context, keys []string) ([]Row, error) {
	if keys != nil {
		keys = store.KeepIDs(keys)
	}

	watched, err := s.queries.ListWatchedAgents(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched agent rows: %w", err)
	}

	return fromWatched(watched), nil
}
