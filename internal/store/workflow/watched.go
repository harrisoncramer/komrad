package workflow

import (
	"context"
	"fmt"

	"komrad/db/models"
	"komrad/internal/watch"
	"komrad/lib/xslice"
	watchv1 "komrad/proto/gen/watch/v1"
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
			Table:  "workflow_event_prompts",
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

	ids := xslice.Map(rows, func(row *models.ListWatchedWorkflowsRow) string { return row.Workflow.ID })

	hooks, err := s.hooksForWorkflows(ctx, ids)
	if err != nil {
		return nil, err
	}

	prompts, err := s.eventPromptsForWorkflows(ctx, ids)
	if err != nil {
		return nil, err
	}

	return xslice.Map(rows, func(row *models.ListWatchedWorkflowsRow) Workflow {
		workflow := workflowFromRow(row.Workflow, row.WorkflowConfig, row.WorkflowProvisionerConfig, hooks[row.Workflow.ID])
		workflow.EventPrompts = prompts[row.Workflow.ID]

		return workflow
	}), nil
}
