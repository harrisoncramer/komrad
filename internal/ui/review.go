package ui

import (
	"time"

	"weave/internal/review"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/bubbles/textarea"
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

	// cursor lives here rather than in the stage, since the sidebar draws the thread it stands on.
	cursor int
	pan    int
	// top is the first row the pane is showing, held rather than derived from the cursor, since
	// deriving it centres the cursor and repaints every row on every move.
	top       int
	frames    *reviewFrames
	composing bool
	// releasing is set while the panel is asking what to send, which is one editor for a word
	// about the work as a whole and one choice of verdict.
	releasing bool
	verdict   int
	editor    textarea.Model

	// filter is the pattern being searched for in the diff, kept apart from the agents list's own
	// filter so a pattern meant for code does not empty that list.
	filter filterState

	// pinTop holds the window where a file jump put it until the cursor moves, since the next
	// repaint would otherwise drag it back to keep the cursor clear of the edge.
	pinTop bool

	// pinFloor is the row a file jump put at the top of the pane, which the window stays at or
	// below while the cursor is inside that file, since the margin would otherwise scroll the file
	// it was jumped away from back into view on the first keypress.
	pinFloor int
	// editing is the thread a composed message is going into, empty when it will open a new
	// thread on the line under the cursor.
	editing string
	loading bool
	notice  string
	// since says how much of the diff is new to the reader, which is a standing fact about what
	// is on screen rather than an answer to a keypress, so it outlives the notice slot.
	since string
	err   error

	// roundDir is where the round has been checked out for the editor to read, named after the
	// agent rather than the round so that it outlives a reload and is still there to be taken
	// away when the screen closes.
	roundDir string
	// roundSha is the snapshot that checkout is standing on, which is what says whether it is
	// still the round being read, since the directory is named after the agent rather than the
	// round and outlives any one of them.
	roundSha string
	// materialising is set while that checkout is being made, so a second press does not start a
	// second one against the same directory.
	materialising bool
	// replacing is the checkout of an earlier round weave has already said it would take, so the
	// press after that warning goes through rather than being asked about again.
	replacing string
	// suggestions are the runs the reviewer wrote into the checkout, each waiting for a comment
	// of its own to carry it to the agent.
	suggestions []pendingSuggestion
	// settled is every proposal already sent or turned down, so that reading the checkout again
	// does not offer back what the reviewer has already answered for.
	settled map[string]bool
	// adrift is what the reviewer wrote on lines the diff does not draw, which stands on screen
	// rather than answering a keypress, since the text is still in the checkout and saying so
	// once would scroll away before they could act on it.
	adrift []review.Suggestion

	// restore is where the cursor was standing when the patch was read again, so a reload lands
	// the reader back where they left rather than at the top of the diff.
	restore *review.Place

	// activity is the conversation as it stood when the sidebar was last drawn, which a tick
	// compares against so a refresh that would find nothing new never rebuilds anything.
	activity reviewActivity

	// polling is set while a conversation read is in flight, so a slow one is not asked for
	// again by every tick that passes underneath it.
	polling bool

	// moved is set once the agent has written something since the round on screen was taken, which
	// is a prompt to open the next one rather than anything the panel does on its own.
	moved bool

	// diffPolling is set while a background probe of the worktree is in flight, and diffPolled
	// is when the last one started, since the worktree is asked every few seconds rather than
	// every tick.
	diffPolling bool
	diffPolled  time.Time
}

// doesTakeEveryKey reports whether the review is mid-input, which means a key bound elsewhere in
// the panel belongs here instead.
func (r reviewState) doesTakeEveryKey() bool {
	return r.composing || r.releasing || r.filter.typing
}

// pendingSuggestion is an edit of the round waiting for a comment to carry it, holding the place
// it is pinned to as well as the lines, since an edit is anchored where it was made rather than
// where the cursor has since got to.
type pendingSuggestion struct {
	anchor review.Anchor
	lines  []string
}

// key is what settles this proposal, matching the suggestion it was built from so that reading the
// checkout again does not offer it back.
func (p pendingSuggestion) key() review.Suggestion {
	return review.Suggestion{
		FilePath: p.anchor.FilePath,
		NewStart: p.anchor.NewStart,
		NewEnd:   p.anchor.NewEnd,
		Lines:    p.lines,
	}
}
