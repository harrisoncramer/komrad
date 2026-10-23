package review

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"komrad/internal/diff"
	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"
)

var errCommentSnapshotGone = errors.New("the snapshot this comment was written on is gone")

type commentOrigin struct {
	ThreadID   string
	SnapshotID string
	Ordinal    int32
	BaseSHA    string
	landed     bool
	fellBack   bool
	// held is the range on screen before the jump, which the way back puts back.
	held  diffSpec
	place *diffview.Place
}

func (r *Screen) toggleJump() screen.Request {
	if r.diff.spec.origin != nil {
		return r.leaveOrigin()
	}

	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return nil
	}

	snapshot := findSnapshot(r.diff.snapshots, thread.GetSnapshotId())
	if snapshot == nil {
		return screen.NewPostMessage(screen.Warning, errCommentSnapshotGone.Error())
	}

	origin := &commentOrigin{
		ThreadID:   thread.GetId(),
		SnapshotID: snapshot.GetId(),
		Ordinal:    snapshot.GetOrdinal(),
		BaseSHA:    thread.GetBaseSha(),
		held:       r.diff.spec,
	}
	if place, ok := r.diff.View().GetPlace(r.diff.Cursor()); ok {
		origin.place = &place
	}

	r.diff.spec = originSpec(origin)

	return r.reload()
}

func (r *Screen) leaveOrigin() screen.Request {
	origin := r.diff.spec.origin
	r.diff.spec = origin.held

	cmd := r.reload()
	r.diff.restore = origin.place

	return cmd
}

func originLabel(origin *commentOrigin) string {
	said := fmt.Sprintf("where the comment was read %s snapshot %d", glyph.Span, origin.Ordinal)
	if origin.fellBack {
		said += ", shown against the snapshot's base"
	}

	return said
}

func (d diffPane) jumpDesc() string {
	if d.spec.origin != nil {
		return "back"
	}

	return "where it was written"
}

func (d *diffPane) originPlace(view *diffview.View) (diffview.Place, bool) {
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

func (d diffPane) threadsOnScreen(threads []*agentv1.ReviewThread) []*agentv1.ReviewThread {
	if d.hidesAgentThreads {
		threads = withoutAgentThreads(threads)
	}

	return d.threadsInRange(threads)
}

// isThreadOnScreen reports whether the thread is one the diff shows in the range on screen.
func (d diffPane) isThreadOnScreen(threads []*agentv1.ReviewThread, id string) bool {
	return slices.ContainsFunc(d.threadsOnScreen(threads), func(thread *agentv1.ReviewThread) bool { return thread.GetId() == id })
}

type originDiff struct {
	base         string
	head         string
	snapshotBase string
	fellBack     bool
}

func (o originDiff) canFallBack() bool {
	return o.head != "" && !o.fellBack && o.base != o.snapshotBase
}

func isUnreadableBase(ctx context.Context, err error) bool {
	return ctx.Err() == nil && !errors.Is(err, diff.ErrNoChanges) && !errors.Is(err, diff.ErrAllExcluded)
}
