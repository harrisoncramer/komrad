package ui

import (
	"slices"

	"komrad/internal/assemble"
	"komrad/internal/ui/diffview"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
	"google.golang.org/protobuf/proto"
)

// reviewDeliveryStates is how many slots the delivery counts get, one per state the proto
// defines with room over.
const reviewDeliveryStates = 8

// reviewThreads is the conversation in the sidebar and what the reader is writing into it.
type reviewThreads struct {
	list []*agentv1.ReviewThread

	// delivery is how many of the conversation's comments stand in each delivery state.
	delivery  [reviewDeliveryStates]int
	composing bool
	// releasing is set while the panel is asking what to send.
	releasing bool
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

// set holds the conversation and its delivery counts together.
func (t *reviewThreads) set(threads []*agentv1.ReviewThread) {
	t.list = threads
	t.delivery = countDeliveries(threads)
}

// followWorld renders what the world now holds of the review, and reads the diff again when the
// agent has moved past the snapshot on screen.
func (r *reviewScreen) followWorld(m *model) tea.Cmd {
	r.followThreads(m)
	r.followNotices(m)

	return r.followMove(m)
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

	threads := assemble.ReviewThreads(m.listing.world, r.agentID)
	if slices.EqualFunc(threads, r.threads.list, isSameThread) {
		return
	}

	r.applyThreads(m, threads)
}

func isSameThread(a, b *agentv1.ReviewThread) bool {
	return proto.Equal(a, b)
}

func countDeliveries(threads []*agentv1.ReviewThread) [reviewDeliveryStates]int {
	var counts [reviewDeliveryStates]int
	for _, thread := range threads {
		for _, message := range thread.GetMessages() {
			counts[deliverySlot(message.GetDelivery())]++
		}
	}

	return counts
}

// deliverySlot is which count a state belongs in, folding a state this build has never heard of
// into the unspecified slot.
func deliverySlot(state agentv1.ReviewDeliveryState) int {
	if state < 0 || int(state) >= reviewDeliveryStates {
		return 0
	}

	return int(state)
}

func withoutAgentThreads(threads []*agentv1.ReviewThread) []*agentv1.ReviewThread {
	kept := make([]*agentv1.ReviewThread, 0, len(threads))
	for _, thread := range threads {
		if !diffview.IsAgentThread(thread) || diffview.IsHoldingUnsent(thread) {
			kept = append(kept, thread)
		}
	}

	return kept
}
