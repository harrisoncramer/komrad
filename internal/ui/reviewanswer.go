package ui

import (
	"errors"
	"fmt"
	"strings"

	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// errAnswerGone is a load asked for an answer whose rounds the review no longer lists.
var errAnswerGone = errors.New("the rounds this answer stands between are gone")

// reviewAnswer is the answer to one comment on screen: the change between the round the comment
// was written on and the round the reply that addressed it carried.
type reviewAnswer struct {
	ThreadID  string
	FromRound string
	ToRound   string
}

// roundsApartError says an answer stands across a rebase no replay could carry across.
type roundsApartError struct {
	Conflicts []string
}

func (e *roundsApartError) Error() string {
	said := "the answer can't be compared across the rebase"
	if len(e.Conflicts) > 0 {
		said += ", conflicts in " + strings.Join(e.Conflicts, ", ")
	}

	return said
}

// toggleAnswer puts the answer to the comment under the cursor on screen, or goes back to the
// whole change from one already there.
func (r *reviewScreen) toggleAnswer(s panelShared) tea.Cmd {
	if r.diff.origin != nil {
		return r.leaveOrigin(s)
	}

	if r.diff.answer != nil {
		r.diff.widen()

		return r.reload(s)
	}

	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return nil
	}

	answer, ok := findAnswerRound(thread)
	if !ok {
		return nil
	}

	r.diff.widen()
	r.diff.baseLabel = "the answer to this comment"
	r.diff.answer = &reviewAnswer{
		ThreadID:  thread.GetId(),
		FromRound: thread.GetRoundId(),
		ToRound:   answer,
	}

	return r.reload(s)
}

// doesOfferAnswer reports whether the answer key has anything to do where the cursor stands.
func (r reviewScreen) doesOfferAnswer() bool {
	if r.diff.answer != nil {
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
	if d.answer != nil {
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
