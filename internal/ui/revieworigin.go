package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"komrad/internal/diff"
	"komrad/internal/glyph"
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
	fellBack bool
	// held is the range on screen before the jump, which the way back puts back.
	held  diffSpec
	place *diffview.Place
}

func (r *reviewScreen) toggleJump(m *model) tea.Cmd {
	if r.diff.spec.origin != nil {
		return r.leaveOrigin(m)
	}

	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return nil
	}

	round := findRound(r.diff.rounds, thread.GetRoundId())
	if round == nil {
		m.note(messageWarning, errCommentRoundGone.Error())

		return nil
	}

	origin := &reviewOrigin{
		ThreadID: thread.GetId(),
		RoundID:  round.GetId(),
		Ordinal:  round.GetOrdinal(),
		BaseSHA:  thread.GetBaseSha(),
		held:     r.diff.spec,
	}
	if place, ok := r.diff.View().GetPlace(r.diff.Cursor()); ok {
		origin.place = &place
	}

	r.diff.spec = originSpec(origin)

	return r.reload(m)
}

func (r *reviewScreen) leaveOrigin(m *model) tea.Cmd {
	origin := r.diff.spec.origin
	r.diff.spec = origin.held

	cmd := r.reload(m)
	r.diff.restore = origin.place

	return cmd
}

func originLabel(origin *reviewOrigin) string {
	said := fmt.Sprintf("where the comment was read %s round %d", glyph.Span, origin.Ordinal)
	if origin.fellBack {
		said += ", shown against the round's base"
	}

	return said
}

func (d reviewDiff) jumpDesc() string {
	if d.spec.origin != nil {
		return "back"
	}

	return "where it was written"
}

func (d *reviewDiff) originPlace(view *diffview.View) (diffview.Place, bool) {
	if d.spec.origin == nil || d.spec.origin.landed {
		return diffview.Place{}, false
	}

	d.spec.origin.landed = true

	y, ok := view.GetThreadRow(d.spec.origin.ThreadID)
	if !ok {
		return diffview.Place{}, false
	}

	return view.GetPlace(y)
}

func (d reviewDiff) threadsOnScreen(threads []*agentv1.ReviewThread) []*agentv1.ReviewThread {
	if d.hidesAgentThreads {
		threads = withoutAgentThreads(threads)
	}

	return d.threadsInRange(threads)
}

// isThreadOnScreen reports whether the thread is one the diff shows in the range on screen.
func (d reviewDiff) isThreadOnScreen(threads []*agentv1.ReviewThread, id string) bool {
	return slices.ContainsFunc(d.threadsOnScreen(threads), func(thread *agentv1.ReviewThread) bool { return thread.GetId() == id })
}

type originDiff struct {
	base      string
	head      string
	roundBase string
	fellBack  bool
}

func (o originDiff) canFallBack() bool {
	return o.head != "" && !o.fellBack && o.base != o.roundBase
}

func isUnreadableBase(ctx context.Context, err error) bool {
	return ctx.Err() == nil && !errors.Is(err, diff.ErrNoChanges) && !errors.Is(err, diff.ErrAllExcluded)
}
