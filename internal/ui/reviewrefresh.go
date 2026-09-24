package ui

import (
	"context"
	"slices"
	"strings"

	"weave/internal/assemble"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewDeliveryStates is how many slots the delivery counts get, one per state the proto
// defines with room over.
const reviewDeliveryStates = 8

// reviewThreads is the conversation in the sidebar and what the reader is writing into it.
type reviewThreads struct {
	list []*agentv1.ReviewThread

	// activity is the conversation as it stood when the sidebar was last drawn, which a refresh
	// compares against so one finding nothing new rebuilds nothing.
	activity reviewActivity
	// reading is set while a conversation read is in flight.
	reading bool
	// failed is set when the last read failed, and holds the next read back until the watch speaks.
	failed bool
	// seen is the watch's probe of the review as the conversation was last read against it.
	seen string
	// held is a read that landed while the reader was typing, drawn once they stop.
	held *reviewRefreshedMsg

	// owed is the id of the last released review while it still holds comments nobody has
	// settled, empty once a second review would be allowed.
	owed string

	// focus is set while the keys are on the conversation in the sidebar rather than on the
	// diff.
	focus bool
	// scroll is how far up from the end of the conversation the sidebar is showing.
	scroll    int
	composing bool
	// releasing is set while the panel is asking what to send.
	releasing bool
	verdict   int
	editor    reviewInput
	// editing is the thread a composed message is going into, empty when it will open a new
	// thread on the line under the cursor.
	editing string
}

// isOwedAnAnswer reports whether a comment the reader released is still open.
func (t reviewThreads) isOwedAnAnswer() bool {
	return t.owed != ""
}

// set holds the conversation and the probe a refresh compares it against together.
func (t *reviewThreads) set(threads []*agentv1.ReviewThread) {
	t.list = threads
	t.activity = reviewActivityOf(threads)
}

// reviewActivity is the cheap probe a conversation refresh is decided on.
type reviewActivity struct {
	threads  int
	messages int
	// newest is the newest thing said or changed, and the counts beside it are what a stamp cannot
	// see.
	newest string
	// delivery is how many comments stand in each state, read off what the server answered rather
	// than off when a row was last written.
	delivery [reviewDeliveryStates]int
}

// reviewRefreshedMsg is the conversation as a tick found it, drawn only when the probe says
// something has been said since the sidebar was written.
type reviewRefreshedMsg struct {
	agentID string
	threads []*agentv1.ReviewThread
	err     error
	owed    *string
}

// follow reads the conversation again when the watch has seen the review move since the last read.
func (r *reviewScreen) follow(s panelShared) tea.Cmd {
	if r.diff.view == nil || r.threads.reading || r.threads.failed {
		return nil
	}

	probe := reviewProbeOf(s.listing.world, r.agentID)
	if probe == r.threads.seen {
		return nil
	}

	r.threads.seen = probe
	r.threads.reading = true

	return r.refreshThreadsCmd(s)
}

// followWatch reads the conversation again after a batch from the watch, retrying a failed read.
func (r *reviewScreen) followWatch(s panelShared) tea.Cmd {
	if r.threads.failed {
		r.threads.failed = false
		r.threads.seen = ""
	}

	return r.follow(s)
}

// catchUp draws a read held back while the reader was typing, then follows the watch.
func (r *reviewScreen) catchUp(s panelShared) tea.Cmd {
	if held := r.threads.held; held != nil && r.diff.view != nil && !r.takesEveryKey() {
		r.threads.held = nil
		r.drawRefreshed(s, *held)
	}

	return tea.Batch(r.follow(s), r.followMove(s))
}

func (r reviewScreen) refreshThreadsCmd(s panelShared) tea.Cmd {
	agents, agentID := s.api.agents, r.agentID

	return func() tea.Msg {
		threads, err := call(reviewTimeout, func(ctx context.Context) ([]*agentv1.ReviewThread, error) {
			return listReviewThreads(ctx, agents, agentID)
		})

		var owed *string
		if err == nil {
			if reviews, rerr := call(reviewTimeout, func(ctx context.Context) ([]*agentv1.Review, error) {
				return listReviews(ctx, agents, agentID)
			}); rerr == nil {
				id := findLastOwedReview(reviews).GetId()
				owed = &id
			}
		}

		return reviewRefreshedMsg{
			agentID: agentID,
			threads: threads,
			err:     err,
			owed:    owed,
		}
	}
}

func (r *reviewScreen) applyRefreshed(h host, s panelShared, msg reviewRefreshedMsg) {
	if msg.agentID != r.agentID {
		return
	}

	r.threads.reading = false

	if msg.err != nil {
		r.threads.failed = true
		h.report("Could not read the review of "+r.agentName, msg.err)

		return
	}

	if r.diff.view == nil {
		return
	}

	if r.takesEveryKey() {
		r.threads.held = &msg

		return
	}

	r.drawRefreshed(s, msg)
}

func (r *reviewScreen) drawRefreshed(s panelShared, msg reviewRefreshedMsg) {
	if msg.owed != nil {
		r.threads.owed = *msg.owed
	}

	if reviewActivityOf(msg.threads) == r.threads.activity {
		return
	}

	r.applyThreads(s, msg.threads)
}

func reviewActivityOf(threads []*agentv1.ReviewThread) reviewActivity {
	activity := reviewActivity{threads: len(threads)}
	for _, thread := range threads {
		activity.messages += len(thread.GetMessages())
		activity.newest = max(activity.newest, thread.GetUpdatedAt())

		for _, message := range thread.GetMessages() {
			activity.newest = max(activity.newest, message.GetUpdatedAt())
			activity.delivery[deliverySlot(message.GetDelivery())]++
		}
	}

	return activity
}

// deliverySlot is which count a state belongs in, folding a state this build has never heard of
// into the unspecified slot.
func deliverySlot(state agentv1.ReviewDeliveryState) int {
	if state < 0 || int(state) >= reviewDeliveryStates {
		return 0
	}

	return int(state)
}

// reviewProbeOf is what the watch knows of an agent's review: the version on its row, when each
// review of its work last moved, and the review notices still waiting for it.
func reviewProbeOf(world assemble.World, agentID string) string {
	var waiting []string
	for id, mail := range world.Mail {
		if mail.GetToAgentId() == agentID && mail.GetKind() == agentv1.MailKind_MAIL_KIND_REVIEW_ACTIVITY {
			waiting = append(waiting, id)
		}
	}
	slices.Sort(waiting)

	var reviews []string
	for id, review := range world.Reviews {
		if review.GetAgentId() == agentID {
			reviews = append(reviews, strings.Join([]string{id, review.GetUpdatedAt(), review.GetReleasedAt(), review.GetDeliveredAt()}, "@"))
		}
	}
	slices.Sort(reviews)

	return world.Agents[agentID].GetReviewVersion() + " " + strings.Join(reviews, ",") + " " + strings.Join(waiting, ",")
}
