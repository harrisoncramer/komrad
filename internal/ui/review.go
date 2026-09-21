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
	threads   []*agentv1.ReviewThread
	loading   bool
	notice    string

	// diff is the patch on screen and where the reader is standing in it.
	diff reviewDiff

	composing bool
	// releasing is set while the panel is asking what to send.
	releasing bool
	verdict   int
	editor    reviewInput

	// threadFocus is set while the keys are on the conversation in the sidebar rather than on the
	// diff.
	threadFocus bool
	// threadScroll is how far up from the end of the conversation the sidebar is showing.
	threadScroll int

	// editing is the thread a composed message is going into, empty when it will open a new
	// thread on the line under the cursor.
	editing string
	// atEditor says your own editor holds the stage, so the panel draws its sidebar alone.
	atEditor    bool
	editingPath string
	// editorPane is the pane the editor was given, empty where the split was refused, which is
	// what says whether a pane going away is the editor being closed.
	editorPane string
	// owed is the id of the last released review while it still holds comments nobody has
	// settled, empty once a second review would be allowed.
	owed string
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

	// activity is the conversation as it stood when the sidebar was last drawn, which a tick
	// compares against so a refresh finding nothing new rebuilds nothing.
	activity reviewActivity

	// polling is set while a conversation read is in flight, so a slow one is not asked for again
	// by every tick that passes underneath it.
	polling bool
}

// doesTakeEveryKey reports whether the review is mid-input, in which case a key bound elsewhere
// in the panel belongs here instead.
func (r reviewState) doesTakeEveryKey() bool {
	return r.composing || r.releasing || r.diff.filter.typing
}

// isOwedAnAnswer reports whether a comment the reader released is still open.
func (r reviewState) isOwedAnAnswer() bool { return r.owed != "" }
