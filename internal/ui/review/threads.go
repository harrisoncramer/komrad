package review

import (
	"slices"

	"komrad/internal/assemble"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"
	watchv1 "komrad/proto/gen/watch/v1"

	"google.golang.org/protobuf/proto"
)

// conversation is the threads on the review and the message the reader is writing into one.
type conversation struct {
	list      []*agentv1.ReviewThread
	composing bool
	editor    inputField
	// editing is the thread a composed message is going into, empty when it will open a new
	// thread on the line under the cursor.
	editing string
	// retrying is the refused message the composer was opened again on, nil otherwise.
	retrying *unsentMessage
	// forge is set while the composer holds a reply going to the forge.
	forge  *forgeCompose
	scroll popupScroll
}

func (t *conversation) set(threads []*agentv1.ReviewThread) {
	t.list = threads
}

// followWorld renders what the world now holds of the review.
func (r *Screen) followWorld(in screen.Inputs) screen.Request {
	r.followThreads(in)

	return r.followNotices(in)
}

// doesMoveReview reports whether a batch touched anything the review of an agent renders.
func doesMoveReview(changed assemble.Changed, agentID string) bool {
	return changed.Replaced ||
		len(changed.Keys[watchv1.Resource_RESOURCE_REVIEW_THREAD]) > 0 ||
		len(changed.Keys[watchv1.Resource_RESOURCE_REVIEW]) > 0 ||
		slices.Contains(changed.Keys[watchv1.Resource_RESOURCE_AGENT], agentID)
}

// followThreads renders the conversation the world holds on a live agent, once the reader has stopped
// typing.
func (r *Screen) followThreads(in screen.Inputs) {
	if r.diff.View() == nil || r.TakesEveryKey() {
		return
	}

	if _, live := r.agentRow(in); !live {
		return
	}

	threads := assemble.ReviewThreads(in.Listing.World, r.agentID)
	if slices.EqualFunc(threads, r.threads.list, isSameThread) {
		return
	}

	r.applyThreads(in, threads)
}

func isSameThread(a, b *agentv1.ReviewThread) bool {
	return proto.Equal(a, b)
}

func withoutAgentThreads(threads []*agentv1.ReviewThread) []*agentv1.ReviewThread {
	kept := make([]*agentv1.ReviewThread, 0, len(threads))
	for _, thread := range threads {
		if !diffview.IsAgentThread(thread) {
			kept = append(kept, thread)
		}
	}

	return kept
}
