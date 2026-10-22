package ui

import (
	"context"
	"fmt"

	"komrad/internal/ui/api"
	agentv1 "komrad/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

// listReviewThreads is the whole conversation on a task's diff.
func listReviewThreads(
	ctx context.Context,
	agents api.AgentReads,
	taskID string,
) ([]*agentv1.ReviewThread, error) {
	resp, err := agents.ListReviewThreads(ctx, connect.NewRequest(&agentv1.ListReviewThreadsRequest{
		Task: &taskID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the review of task %q: %w", taskID, err)
	}

	return resp.Msg.GetThreads(), nil
}

// listReviewViewedFiles is the files of a task the person reviewing has already read.
func listReviewViewedFiles(
	ctx context.Context,
	agents api.AgentReads,
	taskID string,
) ([]*agentv1.ReviewViewedFile, error) {
	resp, err := agents.ListReviewViewedFiles(ctx, connect.NewRequest(&agentv1.ListReviewViewedFilesRequest{
		Task: taskID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read which files you have already read: %w", err)
	}

	return resp.Msg.GetViewed(), nil
}
