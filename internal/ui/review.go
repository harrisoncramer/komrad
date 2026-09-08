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

	// cursor lives here rather than in the stage.
	cursor int
	pan    int
	// top is the first row the pane is showing, held rather than derived from the cursor.
	top       int
	composing bool
	// releasing is set while the panel is asking what to send, which is one editor for a word
	// about the work as a whole and one choice of verdict.
	releasing bool
	verdict   int
	editor    textarea.Model

	// filter is the pattern being searched for in the diff, kept apart from the agents list's own
	// filter so a pattern meant for code does not empty that list.
	filter filterState

	// threadFocus is set while the keys are on the conversation in the sidebar rather than on the
	// diff, which is the only way a thread taller than the pane can be read to its start.
	threadFocus bool
	// threadScroll is how far up from the end of the conversation the sidebar is showing.
	threadScroll int

	// pinTop holds the window where a file jump put it until the cursor moves.
	pinTop bool

	// pinFloor is the row a file jump put at the top of the pane, which the window stays at or
	// below while the cursor is inside that file.
	pinFloor int
	// editing is the thread a composed message is going into, empty when it will open a new
	// thread on the line under the cursor.
	editing string
	loading bool
	notice  string
	// since says how much of the diff is new to the reader, which is a standing fact about what
	// is on screen rather than an answer to a keypress, so it outlives the notice slot.
	since string

	// baseLabel names the round the diff is narrowed to for the band, empty while the diff is
	// measured from the fork point.
	baseLabel string
	// baseRound is the round the narrowed base came from, nil while there is none to offer.
	baseRound *agentv1.ReviewRound
	// baseWanted is the round the diff is narrowed to, held as an id rather than a commit so that
	// each load resolves it against the round it opens.
	baseWanted string
	err        error

	// roundDir is where the round has been checked out for the editor to read, named after the
	// agent rather than the round.
	roundDir string
	// roundSha is the snapshot that checkout is standing on, which is what says whether it is
	// still the round being read.
	roundSha string
	// materialising is set while that checkout is being made, so a second press does not start a
	// second one against the same directory.
	materialising bool
	// replacing is the checkout of an earlier round weave has already said it would take, so the
	// press after that warning goes through rather than being asked about again.
	replacing string
	// harvesting is set while what the reviewer wrote is being read back and written as threads,
	// so a second editor closing does not start a harvest beside the one in flight.
	harvesting bool
	// oversized is the harvest weave refused for being large enough to be a formatter rather than
	// a person, and held for the next press to write.
	oversized bool

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

	// diffPolling is set while a background probe of the worktree is in flight, and diffPolled is
	// when the last one started.
	diffPolling bool
	diffPolled  time.Time
}

// doesTakeEveryKey reports whether the review is mid-input, which means a key bound elsewhere in
// the panel belongs here instead.
func (r reviewState) doesTakeEveryKey() bool {
	return r.composing || r.releasing || r.filter.typing
}
