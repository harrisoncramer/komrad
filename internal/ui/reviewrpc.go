package ui

import (
	"context"
	"fmt"

	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
)

// listReviews is every review given on an agent, read once by the load and handed to everything
// that answers a question about it.
func listReviews(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	agentID string,
) ([]*agentv1.Review, error) {
	resp, err := agents.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{
		AgentId: agentID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the reviews of %q: %w", agentID, err)
	}

	return resp.Msg.GetReviews(), nil
}

// listReviewRounds is every round taken of an agent, oldest first.
func listReviewRounds(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	agentID string,
) ([]*agentv1.ReviewRound, error) {
	resp, err := agents.ListReviewRounds(ctx, connect.NewRequest(&agentv1.ListReviewRoundsRequest{
		AgentId: agentID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the rounds of %q: %w", agentID, err)
	}

	return resp.Msg.GetRounds(), nil
}

// listReviewThreads is the whole conversation on an agent's diff.
func listReviewThreads(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	agentID string,
) ([]*agentv1.ReviewThread, error) {
	resp, err := agents.ListReviewThreads(ctx, connect.NewRequest(&agentv1.ListReviewThreadsRequest{
		AgentId: &agentID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the review of %q: %w", agentID, err)
	}

	return resp.Msg.GetThreads(), nil
}

// listReviewViewedFiles is the files of an agent the person reviewing has already read.
func listReviewViewedFiles(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	agentID string,
) ([]*agentv1.ReviewViewedFile, error) {
	resp, err := agents.ListReviewViewedFiles(ctx, connect.NewRequest(&agentv1.ListReviewViewedFilesRequest{
		AgentId: agentID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read which files you have already read: %w", err)
	}

	return resp.Msg.GetViewed(), nil
}
