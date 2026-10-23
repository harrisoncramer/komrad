package review

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"komrad/internal/glyph"
	"komrad/internal/ui/screen"
	"komrad/lib/dates"
	agentv1 "komrad/proto/gen/agent/v1"
)

// legendFloor is the shortest pane the expanded legend fits in once it carries the step row.
const legendFloor = 21

// taskStartSaid names the start of a range that runs from where the task starts.
const taskStartSaid = "where the task starts"

// steps are the ranges between the snapshots the person commented on, oldest first, the last
// ending on the newest snapshot when the person has not commented on it.
func (d diffPane) steps(threads []*agentv1.ReviewThread) []diffSpec {
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
func (d diffPane) stepAt(steps []diffSpec) int {
	if d.spec.isWholeChange() {
		return -1
	}

	return slices.IndexFunc(steps, d.spec.isSameRange)
}

// stepBase walks back a review, from the whole change to the work since the person's last review
// and on into older ones, stopping on the oldest.
func (r *Screen) stepBase() screen.Request {
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
func (r *Screen) stepBaseForward() screen.Request {
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
func (r *Screen) showRange(from, to string) screen.Request {
	next := diffSpec{from: from, to: r.diff.endOf(to)}
	if next.isSameRange(r.diff.spec) {
		return nil
	}

	r.diff.spec = next

	return r.reload()
}

// endOf is a snapshot as the end of a range, the newest snapshot standing for whichever one is newest.
func (d diffPane) endOf(id string) string {
	if id == d.newest.GetId() {
		return ""
	}

	return id
}

// endOnNewest keeps the start of the range on screen and ends it on the newest snapshot.
func (r *Screen) endOnNewest() screen.Request {
	return r.showRange(r.diff.rangeStart(), "")
}

// rangeStart is the snapshot the range on screen starts after, as a range of snapshots holds it.
func (d diffPane) rangeStart() string {
	if !d.spec.isRange() {
		return ""
	}

	return d.spec.from
}

// rangeEnd is the snapshot the range on screen ends on, the newest one for anything but a range.
func (d diffPane) rangeEnd() string {
	if !d.spec.isRange() {
		return ""
	}

	return d.spec.to
}

// ordinalOf is a snapshot's number, the newest snapshot's for an empty end.
func (d diffPane) ordinalOf(id string) int32 {
	if id == "" {
		return d.newest.GetOrdinal()
	}

	if snapshot := findSnapshot(d.snapshots, id); snapshot != nil {
		return snapshot.GetOrdinal()
	}

	return 0
}

// endSnapshot is the snapshot the range on screen ends on.
func (d diffPane) endSnapshot() *agentv1.ReviewSnapshot {
	if end := d.rangeEnd(); end != "" {
		if snapshot := findSnapshot(d.snapshots, end); snapshot != nil {
			return snapshot
		}
	}

	return d.newest
}

// isHistory reports whether the range ends before the snapshot new comments go on.
func (d diffPane) isHistory() bool {
	switch {
	case d.spec.origin != nil:
		return d.spec.origin.SnapshotID != d.newest.GetId()
	case d.spec.answer != nil:
		return d.spec.answer.ToSnapshot != d.newest.GetId()
	}

	return !d.spec.endsOnNewest()
}

// rangeSaid names both ends of the diff on screen.
func (d diffPane) rangeSaid() string {
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
func (d diffPane) isAcrossBases() bool {
	start := findSnapshot(d.snapshots, d.rangeStart())

	return start != nil && start.GetBaseSha() != d.endSnapshot().GetBaseSha()
}

// newestSaid names the snapshot new comments go on, empty while the range ends on it.
func (d diffPane) newestSaid() string {
	if !d.isHistory() || d.newest == nil {
		return ""
	}

	return fmt.Sprintf("new comments go on snapshot %d", d.newest.GetOrdinal())
}

// widen puts the diff back to the whole change.
func (d *diffPane) widen() {
	d.spec = wholeChange()
}

// doesWalk says whether the step keys have anywhere to go, which an editor holding the stage takes
// away.
func (r Screen) doesWalk() bool {
	return !r.edit.atEditor && len(r.diff.steps(r.threads.list)) > 0
}

// doesFitBaseLegend reports whether the step keys are worth a legend row.
func (r Screen) doesFitBaseLegend(in screen.Inputs) bool {
	if in.Room.HelpExpanded && in.Room.Height > 0 && in.Room.Height < legendFloor {
		return false
	}

	return r.doesWalk()
}

// threadsInRange drops the threads opened after the snapshot the range ends on.
func (d diffPane) threadsInRange(threads []*agentv1.ReviewThread) []*agentv1.ReviewThread {
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

// findLaterAt is when the first snapshot after both a thread's own snapshot and its first message was
// taken, the zero time when no snapshot has followed it.
func findLaterAt(thread *agentv1.ReviewThread, snapshots []*agentv1.ReviewSnapshot) time.Time {
	said := thread.GetMessages()
	if len(said) == 0 {
		return time.Time{}
	}

	first := dates.Read(said[0].GetCreatedAt())
	if first.IsZero() {
		return time.Time{}
	}

	var later time.Time
	for _, snapshot := range snapshots {
		taken := dates.Read(snapshot.GetCreatedAt())
		if snapshot.GetOrdinal() <= thread.GetSnapshotOrdinal() || !first.Before(taken) {
			continue
		}
		if later.IsZero() || taken.Before(later) {
			later = taken
		}
	}

	return later
}

// doesPredate reports whether something was said before a mark, and says yes for a time that is
// not set.
func doesPredate(at, mark time.Time) bool {
	return at.IsZero() || at.Before(mark)
}

// laterAt is the time a thread's later replies are split at, the zero time while the diff on
// screen is the whole change or an answer.
func (d diffPane) laterAt(thread *agentv1.ReviewThread) time.Time {
	if !d.spec.doesSplitReplies() {
		return time.Time{}
	}

	return findLaterAt(thread, d.snapshots)
}

// viewedOffSaid is why a viewed mark is drawn off, empty on the whole change.
func (d diffPane) viewedOffSaid() string {
	if d.spec.isWholeChange() {
		return ""
	}

	return "viewed marks belong to the whole change"
}

// jumpOffSaid is why the jump to where a comment was read is drawn off, empty when it would go.
func (d diffPane) jumpOffSaid() string {
	if d.spec.origin != nil {
		return ""
	}

	if thread, ok := d.threadAtCursor(); ok && thread.GetMirrorsForge() {
		return "a forge comment was read on the pull request, not on a snapshot"
	}

	return ""
}

// editOffSaid is why opening the editor is drawn off, empty when it would open.
func (r Screen) editOffSaid(in screen.Inputs) string {
	switch {
	case r.edit.atEditor:
		return ""
	case r.isWorktreeRemoved(in):
		return worktreeRemovedSaid
	case !r.isHistory():
		return ""
	}

	return fmt.Sprintf("opens the worktree, not snapshot %d", r.diff.endSnapshot().GetOrdinal())
}
