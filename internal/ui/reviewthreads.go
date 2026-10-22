package ui

import (
	"slices"

	"komrad/internal/assemble"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/screen"
	agentv1 "komrad/proto/gen/agent/v1"

	"google.golang.org/protobuf/proto"
)

// reviewThreads is the conversation in the sidebar and what the reader is writing into it.
type reviewThreads struct {
	list []*agentv1.ReviewThread

	composing bool
	editor    reviewInput
	// editing is the thread a composed message is going into, empty when it will open a new
	// thread on the line under the cursor.
	editing string
	// retrying is the refused message the composer was opened again on, nil otherwise.
	retrying *reviewUnsent
	// forge is set while the composer holds a reply going to the forge.
	forge  *reviewForgeCompose
	scroll popupScroll
}

func (t *reviewThreads) set(threads []*agentv1.ReviewThread) {
	t.list = threads
}

// followWorld renders what the world now holds of the review.
func (r *reviewScreen) followWorld(m *model) screen.Request {
	r.followThreads(m)

	return r.followNotices(m)
}

// followThreads renders the conversation the world holds on a live agent, once the reader has stopped
// typing.
func (r *reviewScreen) followThreads(m *model) {
	if r.diff.View() == nil || r.takesEveryKey() {
		return
	}

	if _, live := r.agentRow(m); !live {
		return
	}

	threads := assemble.ReviewThreads(m.listing.World, r.agentID)
	if slices.EqualFunc(threads, r.threads.list, isSameThread) {
		return
	}

	r.applyThreads(m, threads)
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
