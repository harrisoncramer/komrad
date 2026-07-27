package client

import (
	"context"
	"fmt"

	paginationv1 "claude-agents/proto/gen/pagination/v1"
	workflowv1 "claude-agents/proto/gen/workflow/v1"
	"claude-agents/proto/gen/workflow/v1/workflowv1connect"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"
)

// ListAllWorkflows returns every workflow, following the page tokens the same way
// ListAllAgents does.
func ListAllWorkflows(ctx context.Context, workflows workflowv1connect.WorkflowServiceClient) ([]*workflowv1.Workflow, error) {
	var (
		all    []*workflowv1.Workflow
		cursor string
	)

	for range maxPages {
		page := &paginationv1.Pagination{Limit: proto.Int32(pageSize)}
		if cursor != "" {
			page.Cursor = proto.String(cursor)
		}

		resp, err := workflows.ListWorkflows(ctx, connect.NewRequest(&workflowv1.ListWorkflowsRequest{Pagination: page}))
		if err != nil {
			return nil, fmt.Errorf("failed to list workflows: %w", err)
		}

		all = append(all, resp.Msg.GetWorkflows()...)

		cursor = resp.Msg.GetNextPageToken()
		if cursor == "" {
			return all, nil
		}
	}

	return all, nil
}
