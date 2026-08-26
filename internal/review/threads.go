package review

import (
	"context"
	"fmt"
	"time"

	agentv1 "weave/proto/gen/agent/v1"
)

// ThreadClient is how the viewer reaches the review threads on an agent's diff. The viewer
// holds no copy of its own: a review outlives the popup, so the record is the service's and
// the viewer reads it again each time it opens.
type ThreadClient interface {
	ListReviewThreads(ctx context.Context) ([]*agentv1.ReviewThread, error)
	CreateReviewThread(ctx context.Context, anchor Anchor, body string) (*agentv1.ReviewThread, error)
	AddReviewMessage(ctx context.Context, threadID, body string) (*agentv1.ReviewMessage, error)
	EditReviewMessage(ctx context.Context, id, body string) (*agentv1.ReviewMessage, error)
	ResolveReviewThread(ctx context.Context, id string, resolved bool) (*agentv1.ReviewThread, error)
	DeleteReviewThread(ctx context.Context, id string) error
	NotifyReviewThreads(ctx context.Context, interrupt bool) (int32, error)
}

// anchorKey is where a thread is pinned, as the viewer looks it up: the file and the line on
// whichever side the thread lives on. A thread on a deleted line has no new-side number, so
// the side is part of the key rather than assumed.
type anchorKey struct {
	filePath string

	newLine int32
	oldLine int32
}

func keyForThread(thread *agentv1.ReviewThread) anchorKey {
	return anchorKey{
		filePath: thread.GetFilePath(),
		newLine:  thread.GetNewStart(),
		oldLine:  thread.GetOldStart(),
	}
}

// memoryThreadClient is the store the viewer runs against until the service is wired up. It
// keeps the same shapes the API will hand back, so swapping it out changes nothing above it.
type memoryThreadClient struct {
	threads []*agentv1.ReviewThread

	nextID int
}

func newMemoryThreadClient() *memoryThreadClient { return &memoryThreadClient{} }

func (c *memoryThreadClient) ListReviewThreads(context.Context) ([]*agentv1.ReviewThread, error) {
	return c.threads, nil
}

func (c *memoryThreadClient) CreateReviewThread(
	_ context.Context,
	anchor Anchor,
	body string,
) (*agentv1.ReviewThread, error) {
	c.nextID++

	thread := &agentv1.ReviewThread{
		Id:          fmt.Sprintf("thread-%d", c.nextID),
		FilePath:    anchor.FilePath,
		OldStart:    int32(anchor.OldStart),
		OldEnd:      int32(anchor.OldEnd),
		NewStart:    int32(anchor.NewStart),
		NewEnd:      int32(anchor.NewEnd),
		QuotedLines: anchor.QuotedLines,
		CreatedAt:   time.Now().Format(time.RFC3339),
	}
	thread.Messages = []*agentv1.ReviewMessage{c.newMessage(thread.GetId(), body)}

	c.threads = append(c.threads, thread)

	return thread, nil
}

func (c *memoryThreadClient) AddReviewMessage(
	_ context.Context,
	threadID, body string,
) (*agentv1.ReviewMessage, error) {
	thread := c.find(threadID)
	if thread == nil {
		return nil, fmt.Errorf("no review thread %q", threadID)
	}

	message := c.newMessage(threadID, body)
	thread.Messages = append(thread.Messages, message)
	thread.ResolvedAt = nil

	return message, nil
}

func (c *memoryThreadClient) EditReviewMessage(
	_ context.Context,
	id, body string,
) (*agentv1.ReviewMessage, error) {
	for _, thread := range c.threads {
		for _, message := range thread.GetMessages() {
			if message.GetId() == id {
				message.Body = body

				return message, nil
			}
		}
	}

	return nil, fmt.Errorf("no review message %q", id)
}

func (c *memoryThreadClient) ResolveReviewThread(
	_ context.Context,
	id string,
	resolved bool,
) (*agentv1.ReviewThread, error) {
	thread := c.find(id)
	if thread == nil {
		return nil, fmt.Errorf("no review thread %q", id)
	}

	thread.ResolvedAt = nil
	if resolved {
		stamped := time.Now().Format(time.RFC3339)
		thread.ResolvedAt = &stamped
	}

	return thread, nil
}

func (c *memoryThreadClient) DeleteReviewThread(_ context.Context, id string) error {
	for index, thread := range c.threads {
		if thread.GetId() == id {
			c.threads = append(c.threads[:index], c.threads[index+1:]...)

			return nil
		}
	}

	return fmt.Errorf("no review thread %q", id)
}

func (c *memoryThreadClient) NotifyReviewThreads(_ context.Context, _ bool) (int32, error) {
	return int32(len(c.threads)), nil
}

func (c *memoryThreadClient) newMessage(threadID, body string) *agentv1.ReviewMessage {
	c.nextID++

	return &agentv1.ReviewMessage{
		Id:         fmt.Sprintf("message-%d", c.nextID),
		ThreadId:   threadID,
		AuthorKind: agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN,
		Body:       body,
		CreatedAt:  time.Now().Format(time.RFC3339),
	}
}

func (c *memoryThreadClient) find(id string) *agentv1.ReviewThread {
	for _, thread := range c.threads {
		if thread.GetId() == id {
			return thread
		}
	}

	return nil
}
