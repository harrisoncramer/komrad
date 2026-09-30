package forge

import "time"

// Threads is what a forge answered for the review threads on one pull request.
type Threads struct {
	Threads   []ReviewThread
	Truncated bool
}

// ReviewThread is one conversation a forge holds on a pull request, as the forge holds it.
type ReviewThread struct {
	ProviderID string
	Path       string
	Line       int
	URL        string
	Resolved   bool
	Comments   []ReviewComment
}

// ReviewComment is one comment in a forge review thread.
type ReviewComment struct {
	ProviderID string
	Login      string
	Bot        bool
	Body       string
	WrittenAt  time.Time
	EditedAt   *time.Time
}
