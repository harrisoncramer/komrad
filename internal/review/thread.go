package review

import "context"

// Thread is a review conversation as the viewer needs it: where it is pinned, what it was
// written against, and what has been said. It is the viewer's own shape rather than the
// service's, so how the review is carried over the wire does not reach the drawing code.
type Thread struct {
	ID string

	FilePath string

	OldStart int
	OldEnd   int

	NewStart int
	NewEnd   int

	QuotedLines []string

	Resolved bool

	// UnreadByHuman is true when the agent has replied since you last opened the thread,
	// which is what the gutter marks differently.
	UnreadByHuman bool

	Messages []Message
}

// Message is one thing said in a thread.
type Message struct {
	ID string

	// ByAgent says which side wrote it, which is all the viewer needs to label it.
	ByAgent bool

	Body string
}

// Notification is what telling the agent about the review achieved, which the viewer says out
// loud rather than claiming it landed.
type Notification struct {
	Unread int

	Rang        bool
	Interrupted bool
	Waiting     bool
}

// ThreadClient is how the viewer reaches the review threads on an agent's diff. The viewer
// holds no copy of its own: a review outlives the popup, so the record is the service's and
// the viewer reads it again after every write.
type ThreadClient interface {
	ListReviewThreads(ctx context.Context) ([]Thread, error)
	CreateReviewThread(ctx context.Context, anchor Anchor, body string) error
	AddReviewMessage(ctx context.Context, threadID, body string) error
	MarkReviewThreadRead(ctx context.Context, threadID string) error
	ResolveReviewThread(ctx context.Context, id string, resolved bool) error
	DeleteReviewThread(ctx context.Context, id string) error
	NotifyReviewThreads(ctx context.Context, interrupt bool) (Notification, error)
}

// anchorKey is where a thread is pinned, as the viewer looks it up: the file and the line on
// whichever side the thread lives on. A thread on a deleted line has no new-side number, so
// the side is part of the key rather than assumed.
type anchorKey struct {
	filePath string

	newLine int
	oldLine int
}

func keyForThread(thread Thread) anchorKey {
	return anchorKey{filePath: thread.FilePath, newLine: thread.NewStart, oldLine: thread.OldStart}
}
