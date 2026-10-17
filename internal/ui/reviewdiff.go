package ui

import (
	"komrad/internal/ui/diffview"
	agentv1 "komrad/proto/gen/agent/v1"
)

const answerLabel = "the answer to this comment"

// reviewDiff is the patch on screen and where the reader is standing in it.
type reviewDiff struct {
	diffview.Pane

	// filter is the pattern being searched for in the diff.
	filter filterState

	// restore is where the cursor was standing when the patch was read again.
	restore *diffview.Place
	// expanded is the files rendered whole when the patch was put down to be read again.
	expanded map[string]bool

	// since says how much of the diff is new to the reader.
	since string
	spec  diffSpec
	// baseRound is the round the person last released a review against, nil while there is none.
	baseRound *agentv1.ReviewRound
	// baseAt is when the person released the review that round was given against, in clock terms.
	baseAt string

	// rounds are every round of the review, oldest first.
	rounds []*agentv1.ReviewRound
	// reviews are every review on the task the reader may read, oldest first.
	reviews []*agentv1.Review
	// newest is the round new comments go on.
	newest *agentv1.ReviewRound
	landOn string

	// followedMove is the move the diff last caught up on.
	followedMove string

	// hidesAgentThreads says the conversations agents opened are left out.
	hidesAgentThreads bool
}

// diffSpec is the range the diff on screen runs over.
type diffSpec struct {
	// from is the round the range starts after, empty for where the task starts.
	from string
	// to is the round the range ends on, empty for the newest round.
	to string
	// answer is the answer to a comment on screen, nil while the diff is anything else.
	answer *reviewAnswer
	origin *reviewOrigin

	// base and head are the commits the diff on screen was read between, once it has been read.
	base string
	head string
	// replayed says the older side was carried across a rebase onto the newer one's base.
	replayed bool
	// conflicts is the paths that stopped the older side being carried across a rebase.
	conflicts []string
}

func wholeChange() diffSpec {
	return diffSpec{}
}

func answerSpec(answer *reviewAnswer) diffSpec {
	return diffSpec{answer: answer}
}

func originSpec(origin *reviewOrigin) diffSpec {
	return diffSpec{origin: origin}
}

// isRange reports whether the diff runs between two rounds rather than over an answer or an origin.
func (d diffSpec) isRange() bool {
	return d.answer == nil && d.origin == nil
}

func (d diffSpec) isWholeChange() bool {
	return d.isRange() && d.from == "" && d.to == ""
}

// endsOnNewest reports whether the range ends on the round new comments go on.
func (d diffSpec) endsOnNewest() bool {
	return d.isRange() && d.to == ""
}

func (d diffSpec) doesSplitReplies() bool {
	return !d.isWholeChange() && d.answer == nil
}

func (d diffSpec) isSameRange(other diffSpec) bool {
	return d.isRange() && other.isRange() && d.from == other.from && d.to == other.to
}

func (d *diffSpec) settle(msg reviewLoadedMsg) {
	d.base, d.head = msg.base, msg.head
	d.replayed, d.conflicts = msg.replayed, msg.conflicts

	if d.answer != nil {
		d.base, d.head = msg.answerBase, msg.answerHead
	}

	if d.origin != nil {
		d.base, d.head = msg.originBase, msg.originHead
		d.origin.fellBack = msg.originFellBack
	}
}
