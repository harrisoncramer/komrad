package client

import (
	"context"

	paginationv1 "weave/proto/gen/pagination/v1"
	viewv1 "weave/proto/gen/view/v1"
	workflowv1 "weave/proto/gen/workflow/v1"
	"weave/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
)

// ListAllWorkflows returns every live workflow, following the page tokens until the
// listing is exhausted.
func ListAllWorkflows(ctx context.Context, workflows workflowv1connect.WorkflowServiceClient) ([]*workflowv1.Workflow, error) {
	return listAllWorkflows(ctx, workflows, false)
}

// ListAllArchivedWorkflows returns every archived workflow; the two listings never mix, so
// this is a call of its own rather than a flag the caller has to read back off the rows.
func ListAllArchivedWorkflows(ctx context.Context, workflows workflowv1connect.WorkflowServiceClient) ([]*workflowv1.Workflow, error) {
	return listAllWorkflows(ctx, workflows, true)
}

func listAllWorkflows(ctx context.Context, workflows workflowv1connect.WorkflowServiceClient, archived bool) ([]*workflowv1.Workflow, error) {
	return listAll(ctx, func(ctx context.Context, page *paginationv1.Pagination) ([]*workflowv1.Workflow, string, error) {
		resp, err := workflows.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{
			Pagination: page,
			Archived:   archived,
			View:       viewv1.View_VIEW_FULL,
		}))
		if err != nil {
			return nil, "", err
		}

		return resp.Msg.GetWorkflows(), resp.Msg.GetNextPageToken(), nil
	})
}
