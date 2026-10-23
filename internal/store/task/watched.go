package task

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/internal/watch"
	"komrad/lib/xslice"
	watchv1 "komrad/proto/gen/watch/v1"

	"github.com/google/uuid"
)

// WatchedTasks declares the tasks to the watch.
var WatchedTasks = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_TASK,
	Table:     "tasks",
	KeyColumn: "id",
	Feeders: []watch.Feeder{
		{
			Table:  "agent_runs",
			Column: "task_id",
		},
		{
			Table:  "documents",
			Column: "task_id",
		},
		{
			Table:  "review_snapshots",
			Column: "task_id",
		},
	},
}

// ListWatched returns the tasks among keys, or every one when keys is nil, newest first.
func (s *TaskStore) ListWatched(ctx context.Context, keys []uuid.UUID) ([]Task, error) {
	rows, err := s.queries.ListWatchedTasks(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched tasks: %w", err)
	}

	return xslice.TryMap(rows, func(row *models.ListWatchedTasksRow) (Task, error) {
		task, err := fromCountedRow(&row.Task, row.Snapshots)
		if err != nil {
			return Task{}, err
		}
		task.FailedRun = row.FailedRun
		task.Documents = row.Documents
		task.DocumentsUpdatedAt = row.DocumentsUpdatedAt

		return *task, nil
	})
}
