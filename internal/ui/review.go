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

	// atEditor says your own editor holds the stage, so the panel draws its sidebar alone.
	atEditor    bool
	editingPath string
	// editorPane is the pane the editor was given, empty where the split was refused, which is
	// what says whether a pane going away is the editor being closed.
	editorPane string
	// mode is which of the two things the screen is doing: watching the work, or making a pass
	// over it in which both writing keys are available.
	mode reviewMode
	// paused says the agent is stopped for the pass, whoever stopped it.
	paused bool
	// pausing is set while the panel is asking for the agent to stop.
	pausing bool
	// ourPause says this pass is what stopped the agent, so ending the pass is what lets it go.
	ourPause bool
	// touched are the files the reader opened an editor on during the pass, oldest first, which
	// is what the agent is told about when it is let go.
	touched []string
}

// doesTakeEveryKey reports whether the review is mid-input, in which case a key bound elsewhere
// in the panel belongs here instead.
func (r reviewState) doesTakeEveryKey() bool {
	return r.threads.composing || r.threads.releasing || r.diff.filter.typing
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
