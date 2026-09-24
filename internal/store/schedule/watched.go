package schedule

import (
	"context"

	"weave/internal/watch"
	watchv1 "weave/proto/gen/watch/v1"
)

// WatchedSchedules declares the schedules to the watch.
var WatchedSchedules = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_SCHEDULE,
	Table:     "schedules",
	KeyColumn: "id",
}

// ListWatched returns the live schedules among keys, or every one when keys is nil.
func (s *ScheduleStore) ListWatched(ctx context.Context, keys []string) ([]Schedule, error) {
	if keys == nil {
		return s.List(ctx)
	}

	return s.ListByKey(ctx, keys)
}
