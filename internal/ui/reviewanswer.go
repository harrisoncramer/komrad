package ui

import (
	"strings"

	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

// reviewAnswer is the answer to one comment on screen: the change between the round the comment
// was written on and the round the reply that addressed it carried.
type reviewAnswer struct {
	ThreadID string
	// FromBase is the base the comment's round was taken on.
	FromBase string
	// ToBase is the base the answer's round was taken on.
	ToBase string
}

// roundsApartError says an answer stands across a rebase no replay could settle.
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

	from, to := findRound(r.diff.rounds, thread.GetRoundId()), findRound(r.diff.rounds, answer)
	if from == nil || to == nil {
		r.notice = "the rounds this answer stands between are gone"

		return nil
	}

	r.diff.widen()
	r.diff.base = from.GetSnapshotSha()
	r.diff.head = to.GetSnapshotSha()
	r.diff.baseLabel = "the answer to this comment"
	r.diff.answer = &reviewAnswer{
		ThreadID: thread.GetId(),
		FromBase: from.GetBaseSha(),
		ToBase:   to.GetBaseSha(),
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
	said := thread.GetMessages()
	for i := len(said) - 1; i >= 0; i-- {
		if round := said[i].GetRoundId(); round != "" {
			return round, true
		}
	}

	return "", false
}

func findRound(rounds []*agentv1.ReviewRound, id string) *agentv1.ReviewRound {
	for _, round := range rounds {
		if round.GetId() == id {
			return round
		}
	}

	return nil
}
