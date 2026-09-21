package ui

import (
	"time"

	agentv1 "weave/proto/gen/agent/v1"
)

const reviewTimeout = 30 * time.Second

// reviewState is the review of one agent's diff, while that screen is up.
type reviewState struct {
	agentID   string
	agentName string
	dir       string
	root      string
	exclude   []string
	round     *agentv1.ReviewRound
	loading   bool
	notice    string

	// diff is the patch on screen and where the reader is standing in it.
	diff reviewDiff

	// threads is the conversation in the sidebar and what the reader is writing into it.
	threads reviewThreads

	// pass is the reader's own pass over the work, and the editor it may hold open.
	pass reviewPass
}

// doesTakeEveryKey reports whether the review is mid-input, in which case a key bound elsewhere
// in the panel belongs here instead.
func (r reviewState) doesTakeEveryKey() bool {
	return r.isWriting() || r.diff.filter.typing
}

// isWriting reports whether the reader is composing a message or asking what to send.
func (r reviewState) isWriting() bool {
	return r.threads.composing || r.threads.releasing
}

// isThreadTaken reports whether the reader has taken the keys onto a conversation the cursor is
// standing on.
func (r reviewState) isThreadTaken() bool {
	if !r.threads.focus {
		return false
	}

	_, ok := r.diff.threadAtCursor()

	return ok
}
