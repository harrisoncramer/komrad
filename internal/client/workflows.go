package client

import (
	"context"

	paginationv1 "weave/proto/gen/pagination/v1"
	viewv1 "weave/proto/gen/view/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
)

// ListAllArchivedWorkflows returns every archived workflow, following the page tokens until
// the listing is exhausted.
func ListAllArchivedWorkflows(ctx context.Context, workflows workflowv1connect.WorkflowServiceClient) ([]*workflowv1.Workflow, error) {
	return listAll(ctx, func(ctx context.Context, page *paginationv1.Pagination) ([]*workflowv1.Workflow, string, error) {
		resp, err := workflows.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{
			Pagination: page,
			Archived:   true,
			View:       viewv1.View_VIEW_FULL,
		}))
		if err != nil {
			return nil, "", err
		}

		return resp.Msg.GetWorkflows(), resp.Msg.GetNextPageToken(), nil
	})
}
