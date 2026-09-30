package ui

import (
	"slices"

	"komrad/internal/assemble"
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
	delivery [reviewDeliveryStates]int

	// focus is set while the keys are on the conversation in the sidebar rather than on the
	// diff.
	focus bool
	// scroll is how far up from the end of the conversation the sidebar is showing.
	scroll    int
	composing bool
	// releasing is set while the panel is asking what to send.
	releasing bool
	editor    reviewInput
	// editing is the thread a composed message is going into, empty when it will open a new
	// thread on the line under the cursor.
	editing string
	// retrying is the refused message the composer was opened again on, nil otherwise.
	retrying *reviewUnsent
}

// set holds the conversation and its delivery counts together.
func (t *reviewThreads) set(threads []*agentv1.ReviewThread) {
	t.list = threads
	t.delivery = countDeliveries(threads)
}

// followWorld draws what the world now holds of the review, and reads the diff again when the
// agent has moved past the round on screen.
func (r *reviewScreen) followWorld(s panelShared) tea.Cmd {
	r.followThreads(s)

	return tea.Batch(r.followMove(s), r.followOffers(s))
}

// followThreads draws the conversation the world holds on a live agent, once the reader has stopped
// typing.
func (r *reviewScreen) followThreads(s panelShared) {
	if r.diff.View() == nil || r.takesEveryKey() {
		return
	}

	if _, live := r.agentRow(s); !live {
		return
	}

	threads := assemble.ReviewThreads(s.listing.world, r.agentID)
	if slices.EqualFunc(threads, r.threads.list, isSameThread) {
		return
	}

	r.applyThreads(s, threads)
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
