package workflow

import (
	"context"
	"fmt"

	"weave/db/models"
	"weave/internal/watch"
	"weave/lib/xslice"
	watchv1 "weave/proto/gen/watch/v1"
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
	if keys != nil {
		return s.ListByKey(ctx, keys)
	}

	rows, err := s.queries.SnapshotWatchedWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list the watched workflows: %w", err)
	}

	hooks, err := s.hooksByWorkflow(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to read the hooks of the watched workflows: %w", err)
	}

	return xslice.Map(rows, func(row *models.SnapshotWatchedWorkflowsRow) Workflow {
		return workflowFromRow(row.Workflow, row.WorkflowConfig, row.WorkflowProvisionerConfig, hooks[row.Workflow.ID])
	}), nil
}
