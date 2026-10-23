package review

import (
	"fmt"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"
)

// commentAnswer is the answer to one comment on screen: the change the reply that addressed it
// carried.
type commentAnswer struct {
	ThreadID     string
	FromSnapshot string
	ToSnapshot   string
	// held is the range on screen before the answer, which leaving it puts back.
	held  diffSpec
	place *diffview.Place
}

// toggleAnswer puts the answer to the comment under the cursor on screen, or goes back to the
// range held before it.
func (r *Screen) toggleAnswer() screen.Request {
	if r.diff.spec.origin != nil {
		return r.leaveOrigin()
	}

	if r.diff.spec.answer != nil {
		answer := r.diff.spec.answer
		r.diff.spec = answer.held
		cmd := r.reload()
		r.diff.restore = answer.place

		return cmd
	}

	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return nil
	}

	toSnapshot, ok := findAnswerSnapshot(thread)
	if !ok {
		return nil
	}

	answer := &commentAnswer{
		ThreadID:     thread.GetId(),
		FromSnapshot: thread.GetSnapshotId(),
		ToSnapshot:   toSnapshot,
		held:         r.diff.spec,
	}
	if place, ok := r.diff.View().GetPlace(r.diff.Cursor()); ok {
		answer.place = &place
	}

	r.diff.spec = answerSpec(answer)

	return r.reload()
}

// doesOfferAnswer reports whether the answer key has anything to do where the cursor stands.
func (r Screen) doesOfferAnswer() bool {
	if r.diff.spec.answer != nil {
		return true
	}

	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return false
	}

	_, ok = findAnswerSnapshot(thread)

	return ok
}

// answerDesc names what the answer key does from where the reader stands.
func (d diffPane) answerDesc() string {
	if d.spec.answer != nil {
		return "back"
	}

	return "the answer"
}

// findAnswerSnapshot is the snapshot the newest reply that addressed a comment carried.
func findAnswerSnapshot(thread *agentv1.ReviewThread) (string, bool) {
	answered := findAnswer(thread)
	if answered < 0 {
		return "", false
	}

	return thread.GetMessages()[answered].GetSnapshotId(), true
}

// findAnswer is where the newest reply that addressed a comment sits in its thread, -1 for none.
func findAnswer(thread *agentv1.ReviewThread) int {
	said := thread.GetMessages()
	for i := len(said) - 1; i >= 0; i-- {
		if said[i].GetSnapshotId() != "" {
			return i
		}
	}

	return -1
}

// addressedSaid names the snapshot an addressed reply carried, empty for a snapshot the review no
// longer lists.
func (d diffPane) addressedSaid(message *agentv1.ReviewMessage) string {
	snapshot := findSnapshot(d.snapshots, message.GetSnapshotId())
	if snapshot == nil {
		return ""
	}

	return fmt.Sprintf("addressed in snapshot %d", snapshot.GetOrdinal())
}

func findSnapshot(snapshots []*agentv1.ReviewSnapshot, id string) *agentv1.ReviewSnapshot {
	for _, snapshot := range snapshots {
		if snapshot.GetId() == id {
			return snapshot
		}
	}

	return nil
}

func (d *diffPane) landPlace(view *diffview.View) (diffview.Place, bool) {
	thread := d.landOn
	d.landOn = ""
	if thread == "" {
		return diffview.Place{}, false
	}

	y, ok := view.GetThreadRow(thread)
	if !ok {
		return diffview.Place{}, false
	}

	return view.GetPlace(y)
}
