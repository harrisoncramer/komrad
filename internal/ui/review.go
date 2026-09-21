package ui

import (
	"time"

	"weave/internal/review"
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
	view      *review.View
	threads   []*agentv1.ReviewThread

	// cursor lives here rather than in the stage.
	cursor int
	pan    int
	// top is the first row the pane is showing, held rather than derived from the cursor.
	top       int
	composing bool
	// releasing is set while the panel is asking what to send.
	releasing bool
	verdict   int
	editor    reviewInput

	// filter is the pattern being searched for in the diff, kept apart from the agents list's own
	// filter so a pattern meant for code does not empty that list.
	filter filterState

	// threadFocus is set while the keys are on the conversation in the sidebar rather than on the
	// diff.
	threadFocus bool
	// threadScroll is how far up from the end of the conversation the sidebar is showing.
	threadScroll int

	// expanded is the files the reader has opened out, by path, held here rather than on the view
	// so that catching up with the agent does not draw them tight again.
	expanded map[string]bool

	// pinTop holds the window where a file jump put it until the cursor moves.
	pinTop bool

	// pinFloor is the row a file jump put at the top of the pane, which the window stays at or
	// below while the cursor is inside that file.
	pinFloor int
	// editing is the thread a composed message is going into, empty when it will open a new
	// thread on the line under the cursor.
	editing string
	// atEditor says your own editor holds the stage, so the panel draws its sidebar alone.
	atEditor    bool
	editingPath string
	// editorPane is the pane the editor was given, empty where the split was refused, which is
	// what says whether a pane going away is the editor being closed.
	editorPane string
	loading    bool
	notice     string
	// since says how much of the diff is new to the reader.
	since string

	// base is the commit the diff on screen is measured from, empty for the point the branch
	// left main.
	base string
	// baseLabel names that commit for the band, empty while the diff is measured from the fork
	// point.
	baseLabel string
	// head is the commit the diff on screen is measured to, empty for the newest snapshot.
	head string
	// baseRound is the round the narrowed base came from, nil while there is none to offer.
	baseRound *agentv1.ReviewRound
	// baseAt is when the person released the review that round was given against, in clock terms.
	baseAt string
	// pinned says the diff is the snapshot the reader's unsent comments were written against
	// rather than the work as it now stands.
	pinned bool
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
	// chapters are the rounds of the review the two step keys walk, oldest first.
	chapters []reviewChapter
	// chapter is which of those is on screen, negative for the whole change.
	chapter int

	// restore is where the cursor was standing when the patch was read again, so a reload lands the
	// reader back where they left.
	restore *review.Place

	// activity is the conversation as it stood when the sidebar was last drawn, which a tick
	// compares against so a refresh finding nothing new rebuilds nothing.
	activity reviewActivity

	// polling is set while a conversation read is in flight, so a slow one is not asked for again
	// by every tick that passes underneath it.
	polling bool

	// moved is set once the agent has written something since the round on screen was taken, which
	// prompts the reader to open the next one.
	moved bool

	// diffPolling is set while a background probe of the worktree is in flight, and diffPolled is
	// when the last one started.
	diffPolling bool
	diffPolled  time.Time
}

// doesTakeEveryKey reports whether the review is mid-input, in which case a key bound elsewhere
// in the panel belongs here instead.
func (r reviewState) doesTakeEveryKey() bool {
	return r.composing || r.releasing || r.filter.typing
}

// isOwedAnAnswer reports whether a comment the reader released is still open.
func (r reviewState) isOwedAnAnswer() bool { return r.owed != "" }
