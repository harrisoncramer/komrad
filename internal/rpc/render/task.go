package render

import (
	taskstore "komrad/internal/store/task"
	"komrad/lib/dates"
	taskv1 "komrad/proto/gen/task/v1"
)

// Task draws one task as the wire carries it.
func Task(stored taskstore.Task) *taskv1.Task {
	return &taskv1.Task{
		Id:         stored.ID,
		WorkflowId: stored.WorkflowID,
		Slug:       stored.Slug,
		Title:      stored.Title,
		Goal:       stored.Goal,
		CreatedAt:  dates.Stamp(stored.CreatedAt),
		UpdatedAt:  dates.PreciseStamp(stored.UpdatedAt),
		ArchivedAt: dates.StampPtr(stored.ArchivedAt),
	}
}
