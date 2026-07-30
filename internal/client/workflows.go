package client

import (
	"context"

	paginationv1 "claude-agents/proto/gen/pagination/v1"
	workflowv1 "claude-agents/proto/gen/workflow/v1"
	"claude-agents/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
)

// ListAllWorkflows returns every workflow, following the page tokens until the
// listing is exhausted.
func ListAllWorkflows(ctx context.Context, workflows workflowv1connect.WorkflowServiceClient) ([]*workflowv1.Workflow, error) {
	return listAll(ctx, func(ctx context.Context, page *paginationv1.Pagination) ([]*workflowv1.Workflow, string, error) {
		resp, err := workflows.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{Pagination: page}))
		if err != nil {
			return nil, "", err
		}

		return resp.Msg.GetWorkflows(), resp.Msg.GetNextPageToken(), nil
	})
}
