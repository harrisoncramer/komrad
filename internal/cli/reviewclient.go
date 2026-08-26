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
//
// This is also where the API's shapes stop. The viewer works in its own thread and message
// types, so a change to how the review is carried does not reach the code drawing it.
type reviewThreadClient struct {
	agents agentv1connect.AgentServiceClient

	agentID string
}

func newReviewThreadClient(agents agentv1connect.AgentServiceClient, agentID string) *reviewThreadClient {
	return &reviewThreadClient{agents: agents, agentID: agentID}
}

// ListReviewThreads reads the review without claiming it. Claiming is the agent's to do when
// it fetches the conversation to act on, not the reviewer's for looking at their own notes.
func (c *reviewThreadClient) ListReviewThreads(ctx context.Context) ([]review.Thread, error) {
	resp, err := c.agents.ListReviewThreads(ctx, connect.NewRequest(&agentv1.ListReviewThreadsRequest{
		AgentId: &c.agentID,
	}))
	if err != nil {
		return nil, fmt.Errorf("failed to read the review of %q: %w", c.agentID, err)
	}

	threads := make([]review.Thread, 0, len(resp.Msg.GetThreads()))
	for _, thread := range resp.Msg.GetThreads() {
		threads = append(threads, toViewerThread(thread))
	}

	return threads, nil
}

func (c *reviewThreadClient) CreateReviewThread(ctx context.Context, anchor review.Anchor, body string) error {
	_, err := c.agents.CreateReviewThread(ctx, connect.NewRequest(&agentv1.CreateReviewThreadRequest{
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
		return fmt.Errorf("failed to open a review thread on %q: %w", anchor.FilePath, err)
	}

	return nil
}

func (c *reviewThreadClient) AddReviewMessage(ctx context.Context, threadID, body string) error {
	_, err := c.agents.AddReviewMessage(ctx, connect.NewRequest(&agentv1.AddReviewMessageRequest{
		ThreadId: threadID,
		Body:     body,
	}))
	if err != nil {
		return fmt.Errorf("failed to reply in review thread %q: %w", threadID, err)
	}

	return nil
}

// MarkReviewThreadRead says the person reviewing has seen this conversation, which is what
// opening it means. Without it a thread the agent replied in stays marked unread for ever.
func (c *reviewThreadClient) MarkReviewThreadRead(ctx context.Context, threadID string) error {
	_, err := c.agents.MarkReviewThreadRead(ctx, connect.NewRequest(&agentv1.MarkReviewThreadReadRequest{
		Id: threadID,
	}))
	if err != nil {
		return fmt.Errorf("failed to mark review thread %q read: %w", threadID, err)
	}

	return nil
}

func (c *reviewThreadClient) ResolveReviewThread(ctx context.Context, id string, resolved bool) error {
	_, err := c.agents.ResolveReviewThread(ctx, connect.NewRequest(&agentv1.ResolveReviewThreadRequest{
		Id:       id,
		Resolved: resolved,
	}))
	if err != nil {
		return fmt.Errorf("failed to settle review thread %q: %w", id, err)
	}

	return nil
}

func (c *reviewThreadClient) DeleteReviewThread(ctx context.Context, id string) error {
	_, err := c.agents.DeleteReviewThread(ctx, connect.NewRequest(&agentv1.DeleteReviewThreadRequest{Id: id}))
	if err != nil {
		return fmt.Errorf("failed to delete review thread %q: %w", id, err)
	}

	return nil
}

func (c *reviewThreadClient) NotifyReviewThreads(ctx context.Context, interrupt bool) (review.Notification, error) {
	resp, err := c.agents.NotifyReviewThreads(ctx, connect.NewRequest(&agentv1.NotifyReviewThreadsRequest{
		AgentId:   c.agentID,
		Interrupt: interrupt,
	}))
	if err != nil {
		return review.Notification{}, fmt.Errorf("failed to tell agent %q about its review: %w", c.agentID, err)
	}

	return review.Notification{
		Unread:      int(resp.Msg.GetUnread()),
		Rang:        resp.Msg.GetRang(),
		Interrupted: resp.Msg.GetInterrupted(),
		Waiting:     resp.Msg.GetWaiting(),
	}, nil
}

func toViewerThread(thread *agentv1.ReviewThread) review.Thread {
	answered := review.Thread{
		ID:            thread.GetId(),
		FilePath:      thread.GetFilePath(),
		OldStart:      int(thread.GetOldStart()),
		OldEnd:        int(thread.GetOldEnd()),
		NewStart:      int(thread.GetNewStart()),
		NewEnd:        int(thread.GetNewEnd()),
		QuotedLines:   thread.GetQuotedLines(),
		Resolved:      thread.GetResolvedAt() != "",
		UnreadByHuman: thread.GetUnreadByHuman(),
	}

	for _, message := range thread.GetMessages() {
		answered.Messages = append(answered.Messages, review.Message{
			ID:      message.GetId(),
			ByAgent: message.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT,
			Body:    message.GetBody(),
		})
	}

	return answered
}
