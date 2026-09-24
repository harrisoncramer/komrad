package row

import (
	"context"

	"weave/internal/watch"
	watchv1 "weave/proto/gen/watch/v1"
)

// WatchedAgents declares the agents to the watch.
var WatchedAgents = watch.Resource{
	Enum:           watchv1.Resource_RESOURCE_AGENT,
	Table:          "agents",
	KeyColumn:      "id",
	IgnoredColumns: []string{"state_seen_at", "updated_at"},
	Feeders: []watch.Feeder{
		{
			Table:  "agent_metadata",
			Column: "agent_id",
		},
		{
			Table:  "agent_runs",
			Column: "agent_id",
		},
		{
			Table:  "review_threads",
			Column: "agent_id",
		},
	},
}

// ListWatched returns the rows of the agents among keys that are not being deleted, or of every one when keys is nil.
func (s *RowStore) ListWatched(ctx context.Context, keys []string) ([]Row, error) {
	if keys == nil {
		return s.List(ctx)
	}

	return s.ListByKey(ctx, keys)
}
