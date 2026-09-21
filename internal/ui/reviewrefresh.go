package ui

import (
	"context"
	"time"

	agentstore "weave/internal/store/agent"
	agentv1 "weave/proto/gen/agent/v1"

	tea "github.com/charmbracelet/bubbletea"
)

// reviewDeliveryStates is how many slots the delivery counts get, one per state the proto
// defines with room over.
const reviewDeliveryStates = 8

// reviewDiffPollInterval is how often the worktree behind the diff is read again while the
// screen is up, slower than the tick that draws it.
const reviewDiffPollInterval = 5 * time.Second

// reviewThreads is the conversation in the sidebar and what the reader is writing into it.
type reviewThreads struct {
	list []*agentv1.ReviewThread

	// activity is the conversation as it stood when the sidebar was last drawn, which a tick
	// compares against so a refresh finding nothing new rebuilds nothing.
	activity reviewActivity
	// polling is set while a conversation read is in flight, so a slow one is not asked for again
	// by every tick that passes underneath it.
	polling bool

	// owed is the id of the last released review while it still holds comments nobody has
	// settled, empty once a second review would be allowed.
	owed string

	// focus is set while the keys are on the conversation in the sidebar rather than on the
	// diff.
	focus bool
	// scroll is how far up from the end of the conversation the sidebar is showing.
	scroll int

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
func (t reviewThreads) isOwedAnAnswer() bool { return t.owed != "" }

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

// reviewDiffPolledMsg is the worktree as a background read found it, compared with what is on
// screen rather than drawn over it.
type reviewDiffPolledMsg struct {
	agentID string
	// moved says the agent has written since the round on screen was taken.
	moved bool
	err   error
}

// refreshReviewCmd is what the panel's own tick asks for while the review is up: the
// conversation every time, and the worktree every few seconds.
func (m *model) refreshReviewCmd(now time.Time) tea.Cmd {
	if m.screen != screenReview || m.review.diff.view == nil {
		return nil
	}

	// A reader mid-input is left alone, and the tick after the key that closes them picks the work
	// up.
	if m.review.doesTakeEveryKey() {
		return nil
	}

	var cmds []tea.Cmd

	if !m.review.threads.polling {
		m.review.threads.polling = true
		cmds = append(cmds, m.refreshReviewThreadsCmd())
	}

	if !m.review.diff.diffPolling && now.Sub(m.review.diff.diffPolled) >= reviewDiffPollInterval {
		m.review.diff.diffPolling = true
		m.review.diff.diffPolled = now
		cmds = append(cmds, m.pollReviewDiffCmd())
	}

	return tea.Batch(cmds...)
}

func (m model) refreshReviewThreadsCmd() tea.Cmd {
	agents, agentID := m.api.agents, m.review.agentID

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

// pollReviewDiffCmd asks whether the agent has written anything since the round on screen was
// taken, hashing the worktree rather than reading a second patch.
func (m model) pollReviewDiffCmd() tea.Cmd {
	agentID, dir := m.review.agentID, m.review.dir
	snapshot := m.review.round.GetSnapshotSha()
	repo := m.git

	return func() tea.Msg {
		moved, err := call(reviewTimeout, func(ctx context.Context) (bool, error) {
			probe, err := repo.ProbeTree(ctx, dir)
			if err != nil {
				return false, err
			}

			held, err := repo.GetTree(ctx, dir, snapshot)
			if err != nil {
				return false, err
			}

			return probe != held, nil
		})
		if err != nil {
			return reviewDiffPolledMsg{agentID: agentID, err: err}
		}

		return reviewDiffPolledMsg{agentID: agentID, moved: moved}
	}
}

func (m *model) applyReviewRefreshed(msg reviewRefreshedMsg) {
	if msg.agentID != m.review.agentID {
		return
	}

	m.review.threads.polling = false

	// A read that failed says nothing, and the next tick asks again.
	if msg.err != nil || m.review.diff.view == nil {
		return
	}

	// Input can have started while the read was in flight, and the probe is left where it was
	// so the tick after the reader is done finds this again.
	if m.review.doesTakeEveryKey() {
		return
	}

	if msg.owed != nil {
		m.review.threads.owed = *msg.owed
	}

	if reviewActivityOf(msg.threads) == m.review.threads.activity {
		return
	}

	m.applyReviewThreads(msg.threads)
}

func (m *model) applyReviewDiffPolled(msg reviewDiffPolledMsg) tea.Cmd {
	if msg.agentID != m.review.agentID {
		return nil
	}

	m.review.diff.diffPolling = false

	// A worktree emptied under an open review is a move like any other, so the bar offers to catch
	// up and the reload is what finds nothing to show and backs out to the listing.
	if msg.err != nil {
		return nil
	}

	// A worktree edited back to the round it was cut from is not a move, so the notice goes
	// away rather than standing until the reader opens a round for nothing.
	m.review.diff.moved = msg.moved

	if !msg.moved || !m.doesReviewFollowLive() {
		return nil
	}

	next, cmd := m.reloadReview()
	*m = next

	return cmd
}

// doesReviewFollowLive reports whether the diff on screen is meant to keep up with the agent.
func (m model) doesReviewFollowLive() bool {
	if m.review.pass.mode != modeWatching || m.review.diff.view == nil || m.review.loading {
		return false
	}

	if m.isReviewAgentWriting() {
		return false
	}

	return !m.review.threads.holdsUnsent() && m.review.diff.chapter < 0 && !m.review.diff.pinned
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

// isReviewAgentWriting reports whether the agent whose diff is on screen is putting words in
// its worktree.
func (m model) isReviewAgentWriting() bool {
	item, ok := m.getReviewAgentItem()

	return ok && item.alive && item.agent.State == agentstore.StateThinking
}
