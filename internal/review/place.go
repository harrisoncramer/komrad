package review

import (
	"cmp"
	"slices"

	agentv1 "weave/proto/gen/agent/v1"
)

// changedNotice heads the comments whose code the agent has rewritten or removed.
const changedNotice = "── the code these were about has changed ──"

// outsideNotice heads the comments that are still in the change but not in the narrower diff on
// screen.
const outsideNotice = "── still in the change, outside what is on screen ──"

// placement is how a thread reached the row it is drawn on, which separates a thread the diff
// still agrees with from one the agent has written out from under.
type placement uint8

const (
	// pinned is a thread whose line still reads the way it did when it was written.
	pinned placement = iota

	// ghosted is a thread placed by its old-side line onto code that has changed since, so the
	// place is right and the content is not.
	ghosted

	// adrift is a thread with nothing left to stand on, which keeps a row of its own at the foot
	// of the diff.
	adrift

	// collapsed is a thread in a file that has been read and folded away, which has its line back
	// the moment that file is opened again.
	collapsed
)

// placeArm is one way of finding a thread's row.
type placeArm func(*View, lineIndex, *agentv1.ReviewThread) (int, placement, bool)

// PinThreads works out where each thread is drawn now that the agent has been editing under it,
// and never drops one for having moved.
func (v *View) PinThreads(threads []*agentv1.ReviewThread) {
	v.threads = threads
	v.rows = v.rows[:v.patchRows]
	v.pinned = map[int]*agentv1.ReviewThread{}
	v.placed = map[string]placement{}

	index := v.indexLines()

	// A thread written against the snapshot on screen is offered its row ahead of one traced
	// onto the same row.
	var pending, moved []*agentv1.ReviewThread

	for _, thread := range threads {
		// A thread in a collapsed file has a line to stand on the moment the file is opened
		// again, so it is not adrift and does not go to the foot of the diff.
		if v.viewed[v.pathOf(thread)] {
			v.placed[thread.GetId()] = collapsed

			continue
		}

		if v.isExact(thread) {
			pending = append(pending, thread)

			continue
		}

		moved = append(moved, thread)
	}

	pending = append(pending, moved...)

	// Explainer: the arms are walked outside the threads rather than inside them. A row goes to
	// the first thread that reaches it, so walking thread by thread hands the row to whichever
	// thread the query happened to return first, and a guess from a low arm can take a row a
	// higher one wanted.
	for _, arm := range placeArms() {
		pending = v.placeByArm(index, arm, pending)
	}

	v.appendAdrift(pending)

	// The foot section is rebuilt here, so any search holding row numbers from the old one is
	// run again rather than left pointing at rows that have moved.
	if v.search != nil {
		v.SetPattern(v.search.pattern)
	}
}

// placeArms is the ladder in order of how much the row is worth: where git says the thread's
// lines went, then its old-side line, then its text.
func placeArms() []placeArm {
	return []placeArm{
		(*View).rowByTrace,
		(*View).rowByOld,
		(*View).rowByQuote,
	}
}

// placeByArm gives every thread this arm can place a row, and gives back the ones it could not.
func (v *View) placeByArm(
	index lineIndex,
	arm placeArm,
	threads []*agentv1.ReviewThread,
) []*agentv1.ReviewThread {
	left := make([]*agentv1.ReviewThread, 0, len(threads))
	for _, thread := range threads {
		y, where, ok := arm(v, index, thread)
		if !ok || v.pinned[y] != nil {
			left = append(left, thread)

			continue
		}

		v.pinned[y] = thread
		v.placed[thread.GetId()] = where
	}

	return left
}

// rowByTrace is the row git says the thread's lines became.
func (v *View) rowByTrace(index lineIndex, thread *agentv1.ReviewThread) (int, placement, bool) {
	y, ok := v.tracedRowFor(index, thread)
	if !ok {
		return 0, adrift, false
	}

	return y, v.placementFor(y, quoteOf(thread)), true
}

// rowByOld is the thread's line on the old side, which the base commit holds however much the
// agent rewrites the new one.
func (v *View) rowByOld(index lineIndex, thread *agentv1.ReviewThread) (int, placement, bool) {
	old := int(thread.GetOldStart())
	if old <= 0 || !v.doesShareBase(thread) {
		return 0, adrift, false
	}

	y, ok := index.byOld[oldKey{filePath: thread.GetFilePath(), oldLine: old}]
	if !ok {
		return 0, adrift, false
	}

	return y, v.placementFor(y, quoteOf(thread)), true
}

// rowByQuote is the row drawing the text the thread quoted, which refuses two matches rather
// than guessing between them.
func (v *View) rowByQuote(index lineIndex, thread *agentv1.ReviewThread) (int, placement, bool) {
	quote := quoteOf(thread)
	if quote == "" {
		return 0, adrift, false
	}

	found := index.quoted(thread.GetFilePath(), quote)
	if len(found) != 1 {
		return 0, adrift, false
	}

	return found[0], pinned, true
}

// placementFor is whether the line a thread landed on still reads the way the thread was
// written against it.
func (v *View) placementFor(y int, quote string) placement {
	if text, _ := v.quoteAt(y); doesQuoteMatch(text, quote) {
		return pinned
	}

	return ghosted
}

// doesShareBase reports whether a thread's old-side numbers were counted from the same commit
// this patch measures against.
func (v *View) doesShareBase(thread *agentv1.ReviewThread) bool {
	return thread.GetBaseSha() == v.patch.BaseSHA
}

// appendAdrift gives every thread with nowhere to stand a row at the foot of the diff, sorted by
// where it was written so the section holds still while the cursor is in it.
func (v *View) appendAdrift(threads []*agentv1.ReviewThread) {
	if len(threads) == 0 {
		return
	}

	slices.SortFunc(threads, func(a, b *agentv1.ReviewThread) int {
		return cmp.Or(
			cmp.Compare(a.GetFilePath(), b.GetFilePath()),
			cmp.Compare(lineOf(a), lineOf(b)),
			cmp.Compare(a.GetId(), b.GetId()),
		)
	})

	// A comment loses its line for two unrelated reasons, and reading them in one list is what
	// makes a narrowed diff look like it has thrown every comment away.
	var changed, outside []*agentv1.ReviewThread
	for _, thread := range threads {
		if _, drawn := v.findFile(v.pathOf(thread)); drawn {
			changed = append(changed, thread)

			continue
		}

		outside = append(outside, thread)
	}

	v.appendFootSection(changedNotice, changed)
	v.appendFootSection(outsideNotice, outside)
}

// appendFootSection draws one heading and the threads under it, and nothing for a section with
// no threads in it.
func (v *View) appendFootSection(notice string, threads []*agentv1.ReviewThread) {
	if len(threads) == 0 {
		return
	}

	v.rows = append(v.rows, row{
		kind: metadataRow,
		file: -1,
		hunk: -1,
		line: -1,
		text: notice,
	})

	for _, thread := range threads {
		v.rows = append(v.rows, row{
			kind:   threadRow,
			file:   -1,
			hunk:   -1,
			line:   -1,
			thread: thread,
		})
		v.pinned[len(v.rows)-1] = thread
		v.placed[thread.GetId()] = adrift
	}
}
