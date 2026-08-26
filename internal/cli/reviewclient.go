package cli

import (
	"context"
	"fmt"

	"weave/internal/review"
	agentv1 "weave/proto/gen/agent/v1"
	"weave/proto/gen/agent/v1/agentv1connect"

	"connectrpc.com/connect"
)

// reviewThreadClient reaches the review threads on one agent's diff over the API, which is
// how the viewer holds no copy of its own: a review outlives the popup, so it is read back
// from the service each time rather than kept in the process drawing it.
type reviewThreadClient struct {
	agents agentv1connect.AgentServiceClient

	agentID string
}

func newReviewThreadClient(agents agentv1connect.AgentServiceClient, agentID string) *reviewThreadClient {
	return &reviewThreadClient{agents: agents, agentID: agentID}
}

// ListReviewThreads reads the review without claiming it. Marking read is the agent's to do
// when it fetches the conversation to act on, not the reviewer's for looking at their own
// notes.
func (c *reviewThreadClient) ListReviewThreads(ctx context.Context) ([]*agentv1.ReviewThread, error) {
	resp, err := c.agents.ListReviewThreads(ctx, connect.NewRequest(&agentv1.ListReviewThreadsRequest{
		AgentId: &c.agentID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the review of %q: %w", c.agentID, err)
	}

	return resp.Msg.GetThreads(), nil
}

func (c *reviewThreadClient) CreateReviewThread(
	ctx context.Context,
	anchor review.Anchor,
	body string,
) (*agentv1.ReviewThread, error) {
	resp, err := c.agents.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
		AgentId:     c.agentID,
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		OldEnd:      int32(anchor.OldEnd),
		NewStart:    int32(anchor.NewStart),
		NewEnd:      int32(anchor.NewEnd),
		QuotedLines: anchor.QuotedLines,
		Body:        body,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to open a review thread on %q: %w", anchor.FilePath, err)
	}

	return resp.Msg.GetThread(), nil
}

func (c *reviewThreadClient) AddReviewMessage(
	ctx context.Context,
	threadID, body string,
) (*agentv1.ReviewMessage, error) {
	resp, err := c.agents.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{
		ThreadId: threadID,
		Body:     body,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to reply in review thread %q: %w", threadID, err)
	}

	return resp.Msg.GetMessage(), nil
}

func (c *reviewThreadClient) EditReviewMessage(
	ctx context.Context,
	id, body string,
) (*agentv1.ReviewMessage, error) {
	resp, err := c.agents.EditReviewMessage(ctx, connect.NewRequest(&agentv1.EditReviewMessageRequest{
		Id:   id,
		Body: body,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to rewrite review message %q: %w", id, err)
	}

	return resp.Msg.GetMessage(), nil
}

func (c *reviewThreadClient) ResolveReviewThread(
	ctx context.Context,
	id string,
	resolved bool,
) (*agentv1.ReviewThread, error) {
	resp, err := c.agents.ResolveReviewThread(ctx, connect.NewRequest(&agentv1.ResolveReviewThreadRequest{
		Id:       id,
		Resolved: resolved,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to settle review thread %q: %w", id, err)
	}

	return resp.Msg.GetThread(), nil
}

func (c *reviewThreadClient) DeleteReviewThread(ctx context.Context, id string) error {
	_, err := c.agents.DeleteReviewThread(ctx, connect.NewRequest(&agentv1.DeleteReviewThreadRequest{Id: id}))
	if err != nil {
		return fmt.Errorf("failed to delete review thread %q: %w", id, err)
	}

	return nil
}

func (c *reviewThreadClient) NotifyReviewThreads(ctx context.Context, interrupt bool) (int32, error) {
	resp, err := c.agents.NotifyReviewThreads(ctx, connect.NewRequest(&agentv1.NotifyReviewThreadsRequest{
		AgentId:   c.agentID,
		Interrupt: interrupt,
	}))
	if err != nil {
		return 0, fmt.Errorf("failed to tell agent %q about its review: %w", c.agentID, err)
	}

	return resp.Msg.GetUnread(), nil
}
