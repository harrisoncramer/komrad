package schedule

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/watch"
	"weave/lib/xslice"
	watchv1 "weave/proto/gen/watch/v1"
)

// WatchedSchedules declares the schedules to the watch.
var WatchedSchedules = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_SCHEDULE,
	Table:     "schedules",
	KeyColumn: "id",
	Feeders: []watch.Feeder{
		{
			Table:  "schedule_runs",
			Column: "schedule_id",
		},
	},
}

// ListWatched returns the live schedules among keys, or every one when keys is nil.
func (s *ScheduleStore) ListWatched(ctx context.Context, keys []string) ([]Schedule, error) {
	rows, err := s.queries.ListWatchedSchedules(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched schedules: %w", err)
	}

	schedules := xslice.Map(rows, func(row *models.ListWatchedSchedulesRow) Schedule {
		return scheduleFromRow(row.Schedule)
	})

	return s.attachRuns(ctx, schedules, recentRunsWatched)
}
