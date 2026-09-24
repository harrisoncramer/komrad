package workflow

import (
	"context"
	"fmt"

	"komrade/db/models"
	"komrade/internal/watch"
	"komrade/lib/xslice"
	watchv1 "komrade/proto/gen/watch/v1"
)

// WatchedWorkflows declares the workflows to the watch.
var WatchedWorkflows = watch.Resource{
	Enum:      watchv1.Resource_RESOURCE_WORKFLOW,
	Table:     "workflows",
	KeyColumn: "id",
	Feeders: []watch.Feeder{
		{
			Table:  "workflow_hooks",
			Column: "workflow_id",
		},
		{
			Table: "workflow_configs",
			Lookup: &watch.Lookup{
				ParentColumn: "workflow_config_id",
				ChildColumn:  "id",
			},
		},
		{
			Table: "workflow_provisioner_configs",
			Lookup: &watch.Lookup{
				ParentColumn: "provisioner_config_id",
				ChildColumn:  "id",
			},
		},
	},
}

// ListWatched returns the workflows among keys that are not being deleted, or every one when keys is nil, archived ones included.
func (s *WorkflowStore) ListWatched(ctx context.Context, keys []string) ([]Workflow, error) {
	rows, err := s.queries.ListWatchedWorkflows(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched workflows: %w", err)
	}

	hooks, err := s.hooksForWorkflows(ctx, xslice.Map(rows, func(row *models.ListWatchedWorkflowsRow) string { return row.Workflow.ID }))
	if err != nil {
		return nil, err
	}

	return xslice.Map(rows, func(row *models.ListWatchedWorkflowsRow) Workflow {
		return workflowFromRow(row.Workflow, row.WorkflowConfig, row.WorkflowProvisionerConfig, hooks[row.Workflow.ID])
	}), nil
}
