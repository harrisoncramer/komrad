package ui

import (
	"context"
	"errors"
	"fmt"

	"komrad/internal/diff"
	"komrad/internal/ui/diffview"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

var errCommentRoundGone = errors.New("the round this comment was written on is gone")

type reviewOrigin struct {
	ThreadID string
	RoundID  string
	Ordinal  int32
	BaseSHA  string
	landed   bool
	chapter  int
	answer   *reviewAnswer
	place    *diffview.Place
}

func (r *reviewScreen) toggleJump(s panelShared) tea.Cmd {
	if r.diff.origin != nil {
		return r.leaveOrigin(s)
	}

	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return nil
	}

	round := findRound(r.diff.rounds, thread.GetRoundId())
	if round == nil {
		r.notice = errCommentRoundGone.Error()

		return nil
	}

	origin := &reviewOrigin{
		ThreadID: thread.GetId(),
		RoundID:  round.GetId(),
		Ordinal:  round.GetOrdinal(),
		BaseSHA:  thread.GetBaseSha(),
		chapter:  r.diff.chapter,
		answer:   r.diff.answer,
	}
	if place, ok := r.diff.View().GetPlace(r.diff.Cursor()); ok {
		origin.place = &place
	}

	r.diff.widen()
	r.diff.origin = origin
	r.diff.baseLabel = originLabel(origin, false)

	return r.reload(s)
}

func (r *reviewScreen) leaveOrigin(s panelShared) tea.Cmd {
	origin := r.diff.origin
	r.diff.widen()
	if origin.chapter >= 0 {
		r.diff.showChapter(origin.chapter)
	}

	if origin.answer != nil {
		r.diff.answer = origin.answer
		r.diff.baseLabel = "the answer to this comment"
	}

	cmd := r.reload(s)
	r.diff.restore = origin.place

	return cmd
}

func originLabel(origin *reviewOrigin, fellBack bool) string {
	said := fmt.Sprintf("where this comment was written, round %d", origin.Ordinal)
	if fellBack {
		said += ", shown against the round's base"
	}

	return said
}

func (r reviewScreen) doesOfferJump() bool {
	if r.diff.origin != nil {
		return true
	}

	_, ok := r.diff.threadAtCursor()

	return ok
}

func (d reviewDiff) jumpDesc() string {
	if d.origin != nil {
		return "back"
	}

	return "where it was written"
}

func (d *reviewDiff) originPlace(view *diffview.View) (diffview.Place, bool) {
	if d.origin == nil || d.origin.landed {
		return diffview.Place{}, false
	}

	d.origin.landed = true

	y, ok := view.GetThreadRow(d.origin.ThreadID)
	if !ok {
		return diffview.Place{}, false
	}

	return view.GetPlace(y)
}

func (d reviewDiff) threadsOnScreen(threads []*agentv1.ReviewThread) []*agentv1.ReviewThread {
	if d.origin == nil {
		return threadsByChapter(threadsByChapterParams{
			Threads:  threads,
			Chapters: d.chapters,
			At:       d.chapter,
		})
	}

	kept := make([]*agentv1.ReviewThread, 0, len(threads))
	for _, thread := range threads {
		if thread.GetRoundOrdinal() <= d.origin.Ordinal {
			kept = append(kept, thread)
		}
	}

	return kept
}

type originDiff struct {
	base      string
	head      string
	roundBase string
	fellBack  bool
}

func findOriginDiff(rounds []*agentv1.ReviewRound, origin *reviewOrigin) (originDiff, bool) {
	round := findRound(rounds, origin.RoundID)
	if round == nil {
		return originDiff{}, false
	}

	found := originDiff{
		base:      origin.BaseSHA,
		head:      round.GetSnapshotSha(),
		roundBase: round.GetBaseSha(),
	}
	if found.base == "" {
		found.base = found.roundBase
	}

	return found, true
}

func (o originDiff) canFallBack() bool {
	return o.head != "" && !o.fellBack && o.base != o.roundBase
}

func isUnreadableBase(ctx context.Context, err error) bool {
	return ctx.Err() == nil && !errors.Is(err, diff.ErrNoChanges) && !errors.Is(err, diff.ErrAllExcluded)
}
