package ui

import (
	"context"
	"fmt"
	"time"

	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
)

// releasedAtOf is the clock time the person last let a review go, which is what the screen says
// instead of a round number.
func releasedAtOf(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	round *agentv1.ReviewRound,
) string {
	resp, err := agents.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{
		AgentId: round.GetAgentId(),
	}))
	if err != nil {
		return ""
	}

	last := lastHumanReview(resp.Msg.GetReviews(), round.GetOrdinal())
	if last == nil {
		return ""
	}

	return releasedClock(last.GetReleasedAt())
}

// releasedClock is when a review was let go of, in the terms a reader names it by.
func releasedClock(released string) string {
	stamp, err := time.Parse(time.RFC3339, released)
	if err != nil {
		return ""
	}

	return stamp.Local().Format("3:04PM")
}

// previousReviewedRound is the round the person last released a review against, and nothing when
// they have not reviewed this agent before or when its base has moved since.
func previousReviewedRound(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	round *agentv1.ReviewRound,
) *agentv1.ReviewRound {
	resp, err := agents.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{
		AgentId: round.GetAgentId(),
	}))
	if err != nil {
		return nil
	}

	last := lastHumanReview(resp.Msg.GetReviews(), round.GetOrdinal())
	if last == nil {
		return nil
	}

	rounds, err := agents.ListReviewRounds(ctx, connect.NewRequest(&agentv1.ListReviewRoundsRequest{
		AgentId: round.GetAgentId(),
	}))
	if err != nil {
		return nil
	}

	for _, earlier := range rounds.Msg.GetRounds() {
		if earlier.GetId() != last.GetRoundId() {
			continue
		}

		// Explainer: two rounds measured from different points are not comparable. History was
		// rewritten under the branch between them, so a diff of the two snapshots would carry
		// whatever else moved on the base as though the agent had written it.
		if earlier.GetBaseSha() != round.GetBaseSha() {
			return nil
		}

		return earlier
	}

	return nil
}

// roundsOf is every round of an agent, and nothing when they cannot be read, since tracing is a
// better answer where it is available rather than something the screen depends on.
func roundsOf(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	agentID string,
) []*agentv1.ReviewRound {
	resp, err := agents.ListReviewRounds(ctx, connect.NewRequest(&agentv1.ListReviewRoundsRequest{
		AgentId: agentID,
	}))
	if err != nil {
		return nil
	}

	return resp.Msg.GetRounds()
}

// awaitingReviewAnswer reports whether the person has released a review against the round on
// screen, which is the state the service refuses a second review in.
func awaitingReviewAnswer(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	round *agentv1.ReviewRound,
) bool {
	resp, err := agents.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{
		AgentId: round.GetAgentId(),
	}))
	if err != nil {
		return false
	}

	for _, review := range resp.Msg.GetReviews() {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			continue
		}
		if review.GetReleasedAt() != "" && review.GetRoundOrdinal() == round.GetOrdinal() {
			return true
		}
	}

	return false
}

// lastHumanReview is the newest review the person let go of against a round earlier than this
// one, which is where they last looked.
func lastHumanReview(reviews []*agentv1.Review, ordinal int32) *agentv1.Review {
	var last *agentv1.Review
	for _, review := range reviews {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			continue
		}
		if review.GetReleasedAt() == "" || review.GetRoundOrdinal() >= ordinal {
			continue
		}

		last = review
	}

	return last
}

func listReviewViewedFiles(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	roundID string,
) ([]*agentv1.ReviewViewedFile, error) {
	resp, err := agents.ListReviewViewedFiles(ctx, connect.NewRequest(&agentv1.ListReviewViewedFilesRequest{
		RoundId: roundID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read which files you have already read: %w", err)
	}

	return resp.Msg.GetViewed(), nil
}

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

// pendingRoundParams says whose review is being read.
type pendingRoundParams struct {
	AgentID string
}

// pendingRound is the round a reader's own open batch was opened against, and nothing when they
// have none, so a reader part way through a review keeps reading the snapshot they started on
// rather than being moved onto whatever the agent has written since.
func pendingRound(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	params pendingRoundParams,
) (*agentv1.ReviewRound, bool, error) {
	// Explainer: the batch says which snapshot the review is being written against, and a thread
	// does not. Replying into a thread from an earlier round leaves an unsent message on that
	// thread, so reading the round off the thread would drag the whole diff back to whenever that
	// conversation started.
	roundID, err := openBatchRound(ctx, agents, params.AgentID)
	if err != nil {
		return nil, false, err
	}
	if roundID == "" {
		return nil, false, nil
	}

	rounds, err := agents.ListReviewRounds(ctx, connect.NewRequest(&agentv1.ListReviewRoundsRequest{
		AgentId: params.AgentID,
	}))
	if err != nil {
		return nil, false, fmt.Errorf("failed to read the rounds of %q: %w", params.AgentID, err)
	}

	for _, round := range rounds.Msg.GetRounds() {
		if round.GetId() == roundID {
			return round, true, nil
		}
	}

	return nil, false, nil
}

// openBatchRound is the round the person's unreleased review was opened against, empty when they
// have nothing open.
func openBatchRound(
	ctx context.Context,
	agents agentv1connect.AgentServiceClient,
	agentID string,
) (string, error) {
	resp, err := agents.ListReviews(ctx, connect.NewRequest(&agentv1.ListReviewsRequest{
		AgentId: agentID,
	}))
	if err != nil {
		return "", fmt.Errorf("failed to read the reviews of %q: %w", agentID, err)
	}

	for _, review := range resp.Msg.GetReviews() {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			continue
		}
		if review.GetReleasedAt() == "" {
			return review.GetRoundId(), nil
		}
	}

	return "", nil
}
