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
	if m.screen != screenReview || m.review.view == nil {
		return nil
	}

	// A reader mid-input is left alone, and the tick after the key that closes them picks the work
	// up.
	if m.review.doesTakeEveryKey() {
		return nil
	}

	var cmds []tea.Cmd

	if !m.review.polling {
		m.review.polling = true
		cmds = append(cmds, m.refreshReviewThreadsCmd())
	}

	if !m.review.diffPolling && now.Sub(m.review.diffPolled) >= reviewDiffPollInterval {
		m.review.diffPolling = true
		m.review.diffPolled = now
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

	m.review.polling = false

	// A read that failed says nothing, and the next tick asks again.
	if msg.err != nil || m.review.view == nil {
		return
	}

	// Input can have started while the read was in flight, and the probe is left where it was
	// so the tick after the reader is done finds this again.
	if m.review.doesTakeEveryKey() {
		return
	}

	if msg.owed != nil {
		m.review.owed = *msg.owed
	}

	if reviewActivityOf(msg.threads) == m.review.activity {
		return
	}

	m.applyReviewThreads(msg.threads)
}

func (m *model) applyReviewDiffPolled(msg reviewDiffPolledMsg) tea.Cmd {
	if msg.agentID != m.review.agentID {
		return nil
	}

	m.review.diffPolling = false

	// A worktree emptied under an open review is a move like any other, so the bar offers to catch
	// up and the reload is what finds nothing to show and backs out to the listing.
	if msg.err != nil {
		return nil
	}

	// A worktree edited back to the round it was cut from is not a move, so the notice goes
	// away rather than standing until the reader opens a round for nothing.
	m.review.moved = msg.moved

	if !msg.moved || !m.doesReviewFollowLive() {
		return nil
	}

	next, cmd := m.reloadReview()
	*m = next

	return cmd
}

// doesReviewFollowLive reports whether the diff on screen is meant to keep up with the agent.
func (m model) doesReviewFollowLive() bool {
	if m.review.mode != modeWatching || m.review.view == nil || m.review.loading {
		return false
	}

	// Explainer: catching up reads a round of the tree, so following every save would leave a
	// round behind every few seconds and watching would cost history again, which is the thing
	// this screen is being rebuilt to stop. The end of a turn is the moment the agent has handed
	// the work back, so it is both the cheapest place to catch up and the one worth drawing.
	if m.isReviewAgentWriting() {
		return false
	}

	// Explainer: the three below are each a reason the reader is holding a particular snapshot.
	// Comments are pinned to the round they were written against, an earlier chapter is a
	// photograph the reader chose, and a pinned diff is the one an unsent batch belongs to. Any of
	// them makes redrawing underneath them a way to lose the reader's place or their words.
	return !m.doesReviewHoldUnsent() && m.review.chapter < 0 && !m.review.pinned
}

// setReviewThreads holds the conversation and the probe a refresh compares it against together.
func (m *model) setReviewThreads(threads []*agentv1.ReviewThread) {
	m.review.threads = threads
	m.review.activity = reviewActivityOf(threads)
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
