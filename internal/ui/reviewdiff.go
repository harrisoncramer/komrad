package ui

import (
	"fmt"

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
	// baseRound is the round the narrowed base came from, nil while there is none to offer.
	baseRound *agentv1.ReviewRound
	// baseAt is when the person released the review that round was given against, in clock terms.
	baseAt string
	// pinned says the diff is the snapshot the reader's unsent comments were written against
	// rather than the work as it now stands.
	pinned bool

	// chapters are the rounds of the review the two step keys walk, oldest first.
	chapters []reviewChapter
	// rounds are every round of the review, which the answer key finds a comment's two ends in.
	rounds []*agentv1.ReviewRound
	landOn string

	// followedMove is the move the diff last caught up on.
	followedMove string

	// showsAgentThreads says the conversations agents opened are rendered.
	showsAgentThreads bool
}

type diffSpec struct {
	// base is the commit the diff on screen is measured from, empty for the point the branch
	// left main.
	base string
	// label names that commit for the band, empty while the diff is measured from the fork
	// point.
	label string
	// head is the commit the diff on screen is measured to, empty for the newest snapshot.
	head string
	// chapter is which of the review's chapters is on screen, negative for the whole change.
	chapter int
	// answer is the answer to a comment on screen, nil while the diff is anything else.
	answer *reviewAnswer
	origin *reviewOrigin
}

func wholeChange() diffSpec {
	return diffSpec{chapter: -1}
}

func answerSpec(answer *reviewAnswer) diffSpec {
	return diffSpec{
		label:   answerLabel,
		chapter: -1,
		answer:  answer,
	}
}

func originSpec(origin *reviewOrigin) diffSpec {
	return diffSpec{
		label:   originLabel(origin, false),
		chapter: -1,
		origin:  origin,
	}
}

func (d diffSpec) isChapter() bool {
	return d.chapter >= 0
}

// isNarrowed reports whether the diff on screen is measured over a range rather than the whole
// change.
func (d diffSpec) isNarrowed() bool {
	return d.base != "" || d.head != ""
}

func (d diffSpec) isWholeChange() bool {
	return d.base == "" && d.answer == nil && d.origin == nil
}

func (d diffSpec) doesSplitReplies() bool {
	return d.isChapter() || d.origin != nil
}

func (d diffSpec) historySaid(chapters []reviewChapter) string {
	if d.origin != nil {
		return fmt.Sprintf("round %d", d.origin.Ordinal)
	}

	if !d.isChapter() || d.chapter >= len(chapters) {
		return "an earlier chapter"
	}

	return fmt.Sprintf("chapter %d of %d", d.chapter+1, len(chapters))
}

func (d *diffSpec) settle(msg reviewLoadedMsg) {
	if d.answer != nil {
		d.base, d.head = msg.answerBase, msg.answerHead
		d.label = answerLabel
		if msg.replayed {
			d.label += ", replayed across the rebase"
		}
	}

	if d.origin != nil {
		d.base, d.head = msg.originBase, msg.originHead
		d.label = originLabel(d.origin, msg.originFellBack)
	}
}
