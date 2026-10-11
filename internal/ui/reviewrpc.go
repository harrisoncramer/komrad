package ui

import (
	"context"
	"fmt"

	agentv1 "komrad/proto/gen/agent/v1"

	"connectrpc.com/connect"
)

// listReviews is every review given on a task, read once by the load and handed to everything
// that answers a question about it.
func listReviews(
	ctx context.Context,
	agents agentReads,
	taskID string,
) ([]*agentv1.Review, error) {
	resp, err := agents.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{
		Task: &taskID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the reviews of task %q: %w", taskID, err)
	}

	return resp.Msg.GetReviews(), nil
}

// listReviewRounds is every round taken of a task, oldest first.
func listReviewRounds(
	ctx context.Context,
	agents agentReads,
	taskID string,
) ([]*agentv1.ReviewRound, error) {
	resp, err := agents.ListReviewRounds(ctx, connect.NewRequest(&agentv1.ListReviewRoundsRequest{
		Task: &taskID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the rounds of task %q: %w", taskID, err)
	}

	return resp.Msg.GetRounds(), nil
}

// listReviewThreads is the whole conversation on a task's diff.
func listReviewThreads(
	ctx context.Context,
	agents agentReads,
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
	agents agentReads,
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
