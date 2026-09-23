package ui

import (
	"context"
	"slices"
	"strings"
	"time"

	"weave/internal/assemble"
	"weave/internal/ui/panel"
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

	// activity is the conversation as it stood when the sidebar was last drawn, which a refresh
	// compares against so one finding nothing new rebuilds nothing.
	activity reviewActivity
	// polling is set while a conversation read is in flight.
	polling bool
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

// reviewDiffPolledMsg is the worktree as a background read found it, compared with what is on
// screen rather than drawn over it.
type reviewDiffPolledMsg struct {
	agentID string
	// moved says the agent has written since the round on screen was taken.
	moved bool
	err   error
}

// refresh is what the panel's own tick asks for while the review is up: the worktree every few
// seconds.
func (r *reviewScreen) refresh(s panelShared, now time.Time) tea.Cmd {
	if r.diff.view == nil || r.takesEveryKey() {
		return nil
	}

	if r.diff.diffPolling || now.Sub(r.diff.diffPolled) < reviewDiffPollInterval {
		return nil
	}

	r.diff.diffPolling = true
	r.diff.diffPolled = now

	return r.pollDiffCmd(s)
}

// follow reads the conversation again when the watch has seen the review move since the last read.
func (r *reviewScreen) follow(s panelShared) tea.Cmd {
	if r.diff.view == nil || r.threads.polling {
		return nil
	}

	probe := reviewProbeOf(s.listing.world, r.agentID)
	if probe == r.threads.seen {
		return nil
	}

	r.threads.seen = probe
	r.threads.polling = true

	return r.refreshThreadsCmd(s)
}

// catchUp draws a read held back while the reader was typing, then follows the watch.
func (r *reviewScreen) catchUp(s panelShared) tea.Cmd {
	if held := r.threads.held; held != nil && r.diff.view != nil && !r.takesEveryKey() {
		r.threads.held = nil
		r.drawRefreshed(s, *held)
	}

	return r.follow(s)
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

// pollDiffCmd asks whether the agent has written anything since the round on screen was taken,
// hashing the worktree rather than reading a second patch.
func (r reviewScreen) pollDiffCmd(s panelShared) tea.Cmd {
	agentID, dir := r.agentID, r.dir
	snapshot := r.round.GetSnapshotSha()
	repo := s.git

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

func (r *reviewScreen) applyRefreshed(h host, s panelShared, msg reviewRefreshedMsg) {
	if msg.agentID != r.agentID {
		return
	}

	r.threads.polling = false

	if msg.err != nil {
		r.threads.seen = ""
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

func (r *reviewScreen) applyDiffPolled(s panelShared, msg reviewDiffPolledMsg) tea.Cmd {
	if msg.agentID != r.agentID {
		return nil
	}

	r.diff.diffPolling = false

	// A worktree emptied under an open review is a move like any other, so the bar offers to catch
	// up and the reload is what finds nothing to show and backs out to the listing.
	if msg.err != nil {
		return nil
	}

	// A worktree edited back to the round it was cut from is not a move, so the notice goes
	// away rather than standing until the reader opens a round for nothing.
	r.diff.moved = msg.moved

	if !msg.moved || !r.doesFollowLive(s) {
		return nil
	}

	return r.reload(s)
}

// doesFollowLive reports whether the diff on screen is meant to keep up with the agent.
func (r reviewScreen) doesFollowLive(s panelShared) bool {
	if r.pass.mode != modeWatching || r.diff.view == nil || r.loading {
		return false
	}

	if r.isAgentWriting(s) {
		return false
	}

	return !r.threads.holdsUnsent() && r.diff.chapter < 0 && !r.diff.pinned
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

// isAgentWriting reports whether the agent whose diff is on screen is putting words in its
// worktree.
func (r reviewScreen) isAgentWriting(s panelShared) bool {
	item, ok := r.agentRow(s)

	return ok && item.Alive && item.Agent.State == panel.StateThinking
}

// reviewProbeOf is what the watch knows of an agent's review: the version on its row and the
// review notices still waiting for it, whose claiming is what moves a comment to delivered.
func reviewProbeOf(world assemble.World, agentID string) string {
	var waiting []string
	for id, mail := range world.Mail {
		if mail.GetToAgentId() == agentID && mail.GetKind() == agentv1.MailKind_MAIL_KIND_REVIEW_ACTIVITY {
			waiting = append(waiting, id)
		}
	}
	slices.Sort(waiting)

	return world.Agents[agentID].GetReviewVersion() + " " + strings.Join(waiting, ",")
}
