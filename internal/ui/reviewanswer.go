package ui

import (
	"fmt"

	"komrad/internal/ui/diffview"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// reviewAnswer is the answer to one comment on screen: the change the reply that addressed it
// carried.
type reviewAnswer struct {
	ThreadID  string
	FromRound string
	ToRound   string
}

// toggleAnswer puts the answer to the comment under the cursor on screen, or goes back to the
// whole change from one already there.
func (r *reviewScreen) toggleAnswer(m *model) tea.Cmd {
	if r.diff.spec.origin != nil {
		return r.leaveOrigin(m)
	}

	if r.diff.spec.answer != nil {
		thread := r.diff.spec.answer.ThreadID
		r.diff.widen()
		cmd := r.reload(m)
		r.diff.landOn = thread

		return cmd
	}

	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return nil
	}

	answer, ok := findAnswerRound(thread)
	if !ok {
		return nil
	}

	r.diff.spec = answerSpec(&reviewAnswer{
		ThreadID:  thread.GetId(),
		FromRound: thread.GetRoundId(),
		ToRound:   answer,
	})

	return r.reload(m)
}

// doesOfferAnswer reports whether the answer key has anything to do where the cursor stands.
func (r reviewScreen) doesOfferAnswer() bool {
	if r.diff.spec.answer != nil {
		return true
	}

	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return false
	}

	_, ok = findAnswerRound(thread)

	return ok
}

// answerDesc names what the answer key does from where the reader stands.
func (d reviewDiff) answerDesc() string {
	if d.spec.answer != nil {
		return "all changes"
	}

	return "the answer"
}

// findAnswerRound is the round the newest reply that addressed a comment carried.
func findAnswerRound(thread *agentv1.ReviewThread) (string, bool) {
	answered := findAnswer(thread)
	if answered < 0 {
		return "", false
	}

	return thread.GetMessages()[answered].GetRoundId(), true
}

// findAnswer is where the newest reply that addressed a comment sits in its thread, -1 for none.
func findAnswer(thread *agentv1.ReviewThread) int {
	said := thread.GetMessages()
	for i := len(said) - 1; i >= 0; i-- {
		if said[i].GetRoundId() != "" {
			return i
		}
	}

	return -1
}

// addressedSaid names the round an addressed reply carried, empty for a round the review no
// longer lists.
func (d reviewDiff) addressedSaid(message *agentv1.ReviewMessage) string {
	round := findRound(d.rounds, message.GetRoundId())
	if round == nil {
		return ""
	}

	return fmt.Sprintf("addressed in round %d", round.GetOrdinal())
}

func findRound(rounds []*agentv1.ReviewRound, id string) *agentv1.ReviewRound {
	for _, round := range rounds {
		if round.GetId() == id {
			return round
		}
	}

	return nil
}

func (d *reviewDiff) landPlace(view *diffview.View) (diffview.Place, bool) {
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
