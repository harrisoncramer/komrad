package ui

import (
	"fmt"
	"strings"
	"time"

	"komrad/internal/glyph"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// reviewLegendFloor is the shortest pane the expanded legend fits in once it carries the step row.
const reviewLegendFloor = 21

// taskStartSaid names the start of a range that runs from where the task starts.
const taskStartSaid = "where the task starts"

// reviewStep is one stop of the walk between the person's reviews.
type reviewStep struct {
	from string
	to   string
}

// steps are the ranges between the person's released reviews, oldest first, the last ending on the
// newest round when the person has not reviewed it.
func (d reviewDiff) steps() []reviewStep {
	reviewed := map[string]bool{}
	for _, review := range d.reviews {
		if review.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT && review.GetReleasedAt() != "" {
			reviewed[review.GetRoundId()] = true
		}
	}

	var ends []string
	for _, round := range d.rounds {
		if reviewed[round.GetId()] && round.GetOrdinal() <= d.newest.GetOrdinal() {
			ends = append(ends, round.GetId())
		}
	}

	if !reviewed[d.newest.GetId()] && d.newest != nil {
		ends = append(ends, d.newest.GetId())
	}

	if len(ends) < 2 {
		return nil
	}

	steps := make([]reviewStep, 0, len(ends))
	from := ""
	for _, end := range ends {
		step := reviewStep{from: from, to: end}
		if end == d.newest.GetId() {
			step.to = ""
		}

		steps = append(steps, step)
		from = end
	}

	return steps
}

// stepAt is which of the steps is on screen, -1 for a range that is not one of them.
func (d reviewDiff) stepAt(steps []reviewStep) int {
	if !d.spec.isRange() || d.spec.isWholeChange() {
		return -1
	}

	for at, step := range steps {
		if step.from == d.spec.from && step.to == d.spec.to {
			return at
		}
	}

	return -1
}

// stepBase walks back a review, from the whole change to the work since the person's last review
// and on into older ones, stopping on the oldest.
func (r *reviewScreen) stepBase(m *model) tea.Cmd {
	if r.diff.spec.origin != nil {
		return r.leaveOrigin(m)
	}

	steps := r.diff.steps()
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

	return r.showRange(m, steps[at].from, steps[at].to)
}

// stepBaseForward walks the other way, and stops on the whole change.
func (r *reviewScreen) stepBaseForward(m *model) tea.Cmd {
	if r.diff.spec.origin != nil {
		return r.leaveOrigin(m)
	}

	steps := r.diff.steps()
	at := r.diff.stepAt(steps)
	switch {
	case r.diff.spec.isWholeChange():
		return nil
	case at < 0 || at == len(steps)-1:
		return r.showRange(m, "", "")
	}

	return r.showRange(m, steps[at+1].from, steps[at+1].to)
}

func (r *reviewScreen) clickChange(m *model, key string) tea.Cmd {
	switch key {
	case "back":
		return r.stepBase(m)
	case "forward":
		return r.stepBaseForward(m)
	case "send":
		return r.orRefuseWrite(m, r.openRelease)
	}

	return nil
}

// showRange puts the range between two rounds on screen and reads the diff again once it has moved.
func (r *reviewScreen) showRange(m *model, from, to string) tea.Cmd {
	next := diffSpec{from: from, to: r.diff.endOf(to)}
	if next.isSameRange(r.diff.spec) {
		return nil
	}

	r.diff.spec = next

	return r.reload(m)
}

// endOf is a round as the end of a range, the newest round standing for whichever round is newest.
func (d reviewDiff) endOf(id string) string {
	if id == d.newest.GetId() {
		return ""
	}

	return id
}

// endOnNewest keeps the start of the range on screen and ends it on the newest round.
func (r *reviewScreen) endOnNewest(m *model) tea.Cmd {
	return r.showRange(m, r.diff.rangeStart(), "")
}

// rangeStart is the round the range on screen starts after, as a range of rounds holds it.
func (d reviewDiff) rangeStart() string {
	if !d.spec.isRange() {
		return ""
	}

	return d.spec.from
}

// rangeEnd is the round the range on screen ends on, the newest one for anything but a range.
func (d reviewDiff) rangeEnd() string {
	if !d.spec.isRange() {
		return ""
	}

	return d.spec.to
}

// ordinalOf is a round's number, the newest round's for an empty end.
func (d reviewDiff) ordinalOf(id string) int32 {
	if id == "" {
		return d.newest.GetOrdinal()
	}

	if round := findRound(d.rounds, id); round != nil {
		return round.GetOrdinal()
	}

	return 0
}

// endRound is the round the range on screen ends on.
func (d reviewDiff) endRound() *agentv1.ReviewRound {
	if end := d.rangeEnd(); end != "" {
		if round := findRound(d.rounds, end); round != nil {
			return round
		}
	}

	return d.newest
}

// isHistory reports whether the range ends before the round new comments go on.
func (d reviewDiff) isHistory() bool {
	switch {
	case d.spec.origin != nil:
		return d.spec.origin.RoundID != d.newest.GetId()
	case d.spec.answer != nil:
		return d.spec.answer.ToRound != d.newest.GetId()
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
		start = fmt.Sprintf("round %d", d.ordinalOf(d.spec.from))
	}

	said := fmt.Sprintf("%s %s round %d", start, glyph.Span, d.endRound().GetOrdinal())
	switch {
	case len(d.spec.conflicts) > 0:
		said += ", conflicts in " + strings.Join(d.spec.conflicts, ", ")
	case d.spec.replayed:
		said += ", replayed across the rebase"
	case d.isAcrossBases():
		said += ", the base moved between these rounds"
	}

	return said
}

// isAcrossBases reports whether the range starts on a round taken on another base than the round it
// ends on.
func (d reviewDiff) isAcrossBases() bool {
	start := findRound(d.rounds, d.rangeStart())

	return start != nil && start.GetBaseSha() != d.endRound().GetBaseSha()
}

// newestSaid names the round new comments go on, empty while the range ends on it.
func (d reviewDiff) newestSaid() string {
	if !d.isHistory() || d.newest == nil {
		return ""
	}

	return fmt.Sprintf("new comments go on round %d", d.newest.GetOrdinal())
}

// widen puts the diff back to the whole change.
func (d *reviewDiff) widen() {
	d.spec = wholeChange()
}

// doesWalk says whether the step keys have anywhere to go, which an editor holding the stage takes
// away.
func (r reviewScreen) doesWalk() bool {
	return !r.hold.atEditor && len(r.diff.steps()) > 0
}

// doesFitBaseLegend reports whether the step keys are worth a legend row.
func (r reviewScreen) doesFitBaseLegend(m *model) bool {
	if m.helpExpanded && m.term.height > 0 && m.term.height < reviewLegendFloor {
		return false
	}

	return r.doesWalk()
}

// threadsInRange drops the threads opened after the round the range ends on.
func (d reviewDiff) threadsInRange(threads []*agentv1.ReviewThread) []*agentv1.ReviewThread {
	end := d.endRound().GetOrdinal()
	if d.spec.origin != nil {
		end = d.spec.origin.Ordinal
	}

	if end == 0 || (d.spec.isRange() && d.spec.endsOnNewest()) {
		return threads
	}

	kept := make([]*agentv1.ReviewThread, 0, len(threads))
	for _, thread := range threads {
		if thread.GetRoundOrdinal() <= end {
			kept = append(kept, thread)
		}
	}

	return kept
}

// findLaterStamp is when the first round after both a thread's own round and its first message was
// taken, empty when no round has followed it.
func findLaterStamp(thread *agentv1.ReviewThread, rounds []*agentv1.ReviewRound) string {
	said := thread.GetMessages()
	if len(said) == 0 {
		return ""
	}

	first := said[0].GetCreatedAt()
	stamp := ""
	for _, round := range rounds {
		if round.GetOrdinal() <= thread.GetRoundOrdinal() || !isBefore(first, round.GetCreatedAt()) {
			continue
		}
		if stamp == "" || isBefore(round.GetCreatedAt(), stamp) {
			stamp = round.GetCreatedAt()
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

	return findLaterStamp(thread, d.rounds)
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
		return "a forge comment was read on the pull request, not on a round"
	}

	return ""
}

// editOffSaid is why opening the editor is drawn off, empty when it would open.
func (r reviewScreen) editOffSaid() string {
	if r.hold.atEditor || !r.isHistory() {
		return ""
	}

	return fmt.Sprintf("opens the worktree, not round %d", r.diff.endRound().GetOrdinal())
}
