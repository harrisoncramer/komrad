package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"komrad/internal/glyph"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"
)

// reviewLegendFloor is the shortest pane the expanded legend fits in once it carries the step row.
const reviewLegendFloor = 21

// taskStartSaid names the start of a range that runs from where the task starts.
const taskStartSaid = "where the task starts"

// steps are the ranges between the snapshots the person commented on, oldest first, the last
// ending on the newest snapshot when the person has not commented on it.
func (d reviewDiff) steps(threads []*agentv1.ReviewThread) []diffSpec {
	reviewed := map[string]bool{}
	for _, thread := range threads {
		if thread.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN {
			reviewed[thread.GetSnapshotId()] = true
		}
	}

	var ends []string
	for _, snapshot := range d.snapshots {
		if reviewed[snapshot.GetId()] && snapshot.GetOrdinal() <= d.newest.GetOrdinal() {
			ends = append(ends, snapshot.GetId())
		}
	}

	if d.newest != nil && !reviewed[d.newest.GetId()] {
		ends = append(ends, d.newest.GetId())
	}

	if len(ends) < 2 {
		return nil
	}

	steps := make([]diffSpec, 0, len(ends))
	from := ""
	for _, end := range ends {
		step := diffSpec{from: from, to: end}
		if end == d.newest.GetId() {
			step.to = ""
		}

		steps = append(steps, step)
		from = end
	}

	return steps
}

// stepAt is which of the steps is on screen, -1 for a range that is not one of them.
func (d reviewDiff) stepAt(steps []diffSpec) int {
	if d.spec.isWholeChange() {
		return -1
	}

	return slices.IndexFunc(steps, d.spec.isSameRange)
}

// stepBase walks back a review, from the whole change to the work since the person's last review
// and on into older ones, stopping on the oldest.
func (r *reviewScreen) stepBase() screen.Request {
	if r.diff.spec.origin != nil {
		return r.leaveOrigin()
	}

	steps := r.diff.steps(r.threads.list)
	at := r.diff.stepAt(steps)
	switch {
	case len(steps) == 0:
		return nil
	case at < 0:
		at = len(steps) - 1
	case at == 0:
		return nil
	default:
		at--
	}

	return r.showRange(steps[at].from, steps[at].to)
}

// stepBaseForward walks the other way, and stops on the whole change.
func (r *reviewScreen) stepBaseForward() screen.Request {
	if r.diff.spec.origin != nil {
		return r.leaveOrigin()
	}

	steps := r.diff.steps(r.threads.list)
	at := r.diff.stepAt(steps)
	switch {
	case r.diff.spec.isWholeChange():
		return nil
	case at < 0 || at == len(steps)-1:
		return r.showRange("", "")
	}

	return r.showRange(steps[at+1].from, steps[at+1].to)
}

// showRange puts the range between two snapshots on screen and reads the diff again once it has moved.
func (r *reviewScreen) showRange(from, to string) screen.Request {
	next := diffSpec{from: from, to: r.diff.endOf(to)}
	if next.isSameRange(r.diff.spec) {
		return nil
	}

	r.diff.spec = next

	return r.reload()
}

// endOf is a snapshot as the end of a range, the newest snapshot standing for whichever one is newest.
func (d reviewDiff) endOf(id string) string {
	if id == d.newest.GetId() {
		return ""
	}

	return id
}

// endOnNewest keeps the start of the range on screen and ends it on the newest snapshot.
func (r *reviewScreen) endOnNewest() screen.Request {
	return r.showRange(r.diff.rangeStart(), "")
}

// rangeStart is the snapshot the range on screen starts after, as a range of snapshots holds it.
func (d reviewDiff) rangeStart() string {
	if !d.spec.isRange() {
		return ""
	}

	return d.spec.from
}

// rangeEnd is the snapshot the range on screen ends on, the newest one for anything but a range.
func (d reviewDiff) rangeEnd() string {
	if !d.spec.isRange() {
		return ""
	}

	return d.spec.to
}

// ordinalOf is a snapshot's number, the newest snapshot's for an empty end.
func (d reviewDiff) ordinalOf(id string) int32 {
	if id == "" {
		return d.newest.GetOrdinal()
	}

	if snapshot := findSnapshot(d.snapshots, id); snapshot != nil {
		return snapshot.GetOrdinal()
	}

	return 0
}

// endSnapshot is the snapshot the range on screen ends on.
func (d reviewDiff) endSnapshot() *agentv1.ReviewSnapshot {
	if end := d.rangeEnd(); end != "" {
		if snapshot := findSnapshot(d.snapshots, end); snapshot != nil {
			return snapshot
		}
	}

	return d.newest
}

// isHistory reports whether the range ends before the snapshot new comments go on.
func (d reviewDiff) isHistory() bool {
	switch {
	case d.spec.origin != nil:
		return d.spec.origin.SnapshotID != d.newest.GetId()
	case d.spec.answer != nil:
		return d.spec.answer.ToSnapshot != d.newest.GetId()
	}

	return !d.spec.endsOnNewest()
}

// rangeSaid names both ends of the diff on screen.
func (d reviewDiff) rangeSaid() string {
	switch {
	case d.spec.answer != nil:
		said := answerLabel
		if d.spec.replayed {
			said += ", replayed across the rebase"
		}

		return said
	case d.spec.origin != nil:
		return originLabel(d.spec.origin)
	}

	start := taskStartSaid
	if d.spec.from != "" {
		start = fmt.Sprintf("snapshot %d", d.ordinalOf(d.spec.from))
	}

	said := fmt.Sprintf("%s %s snapshot %d", start, glyph.Span, d.endSnapshot().GetOrdinal())
	switch {
	case len(d.spec.conflicts) > 0:
		said += ", conflicts in " + strings.Join(d.spec.conflicts, ", ")
	case d.spec.replayed:
		said += ", replayed across the rebase"
	case d.isAcrossBases():
		said += ", the base moved between these snapshots"
	}

	return said
}

// isAcrossBases reports whether the range starts on a snapshot taken on another base than the snapshot it
// ends on.
func (d reviewDiff) isAcrossBases() bool {
	start := findSnapshot(d.snapshots, d.rangeStart())

	return start != nil && start.GetBaseSha() != d.endSnapshot().GetBaseSha()
}

// newestSaid names the snapshot new comments go on, empty while the range ends on it.
func (d reviewDiff) newestSaid() string {
	if !d.isHistory() || d.newest == nil {
		return ""
	}

	return fmt.Sprintf("new comments go on snapshot %d", d.newest.GetOrdinal())
}

// widen puts the diff back to the whole change.
func (d *reviewDiff) widen() {
	d.spec = wholeChange()
}

// doesWalk says whether the step keys have anywhere to go, which an editor holding the stage takes
// away.
func (r reviewScreen) doesWalk() bool {
	return !r.edit.atEditor && len(r.diff.steps(r.threads.list)) > 0
}

// doesFitBaseLegend reports whether the step keys are worth a legend row.
func (r reviewScreen) doesFitBaseLegend(m *model) bool {
	if m.helpExpanded && m.term.height > 0 && m.term.height < reviewLegendFloor {
		return false
	}

	return r.doesWalk()
}

// threadsInRange drops the threads opened after the snapshot the range ends on.
func (d reviewDiff) threadsInRange(threads []*agentv1.ReviewThread) []*agentv1.ReviewThread {
	end := d.endSnapshot().GetOrdinal()
	if d.spec.origin != nil {
		end = d.spec.origin.Ordinal
	}

	if end == 0 || (d.spec.isRange() && d.spec.endsOnNewest()) {
		return threads
	}

	kept := make([]*agentv1.ReviewThread, 0, len(threads))
	for _, thread := range threads {
		if thread.GetSnapshotOrdinal() <= end {
			kept = append(kept, thread)
		}
	}

	return kept
}

// findLaterStamp is when the first snapshot after both a thread's own snapshot and its first message was
// taken, empty when no snapshot has followed it.
func findLaterStamp(thread *agentv1.ReviewThread, snapshots []*agentv1.ReviewSnapshot) string {
	said := thread.GetMessages()
	if len(said) == 0 {
		return ""
	}

	first := said[0].GetCreatedAt()
	stamp := ""
	for _, snapshot := range snapshots {
		if snapshot.GetOrdinal() <= thread.GetSnapshotOrdinal() || !isBefore(first, snapshot.GetCreatedAt()) {
			continue
		}
		if stamp == "" || isBefore(snapshot.GetCreatedAt(), stamp) {
			stamp = snapshot.GetCreatedAt()
		}
	}

	return stamp
}

// isBefore reports whether one stamp came before another, parsed rather than compared as text.
func isBefore(stamp, mark string) bool {
	before, err := compareStamps(stamp, mark)

	return err == nil && before
}

// doesPredate reports whether something was said before a mark, and says yes for a stamp it
// cannot read.
func doesPredate(stamp, mark string) bool {
	before, err := compareStamps(stamp, mark)

	return err != nil || before
}

func compareStamps(stamp, mark string) (bool, error) {
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return false, err
	}

	against, err := time.Parse(time.RFC3339, mark)
	if err != nil {
		return false, err
	}

	return at.Before(against), nil
}

// laterStamp is the stamp a thread's later replies are split at, empty while the diff on screen
// is the whole change or an answer.
func (d reviewDiff) laterStamp(thread *agentv1.ReviewThread) string {
	if !d.spec.doesSplitReplies() {
		return ""
	}

	return findLaterStamp(thread, d.snapshots)
}

// viewedOffSaid is why a viewed mark is drawn off, empty on the whole change.
func (d reviewDiff) viewedOffSaid() string {
	if d.spec.isWholeChange() {
		return ""
	}

	return "viewed marks belong to the whole change"
}

// jumpOffSaid is why the jump to where a comment was read is drawn off, empty when it would go.
func (d reviewDiff) jumpOffSaid() string {
	if d.spec.origin != nil {
		return ""
	}

	if thread, ok := d.threadAtCursor(); ok && thread.GetMirrorsForge() {
		return "a forge comment was read on the pull request, not on a snapshot"
	}

	return ""
}

// editOffSaid is why opening the editor is drawn off, empty when it would open.
func (r reviewScreen) editOffSaid(m *model) string {
	switch {
	case r.edit.atEditor:
		return ""
	case r.isWorktreeRemoved(m):
		return worktreeRemovedSaid
	case !r.isHistory():
		return ""
	}

	return fmt.Sprintf("opens the worktree, not snapshot %d", r.diff.endSnapshot().GetOrdinal())
}
