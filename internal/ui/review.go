package ui

import (
	"time"

	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	tea "charm.land/bubbletea/v2"
)

const reviewTimeout = 30 * time.Second

// reviewScreen is the review of one agent's diff, while that screen is up.
type reviewScreen struct {
	agentID   string
	agentName string
	dir       string
	root      string
	exclude   []string
	round     *agentv1.ReviewRound
	loading   bool
	notice    string

	// diff is the patch on screen and where the reader is standing in it.
	diff reviewDiff

	// threads is the conversation in the sidebar and what the reader is writing into it.
	threads reviewThreads

	// hold is the reader's own hold over the work, and the editor it may hold open.
	hold reviewHold

	// offer is the forge thread J last chose, empty until it has chosen one.
	offer string

	offers reviewOffers
}

func (r reviewScreen) parent() screen {
	return screenAgents
}

// crumbs runs through the agent whose work is under review.
func (r reviewScreen) crumbs(panelShared) crumbs {
	return crumbs{trail: []string{r.agentName, "review"}}
}

// takesEveryKey reports whether the review is mid-input, in which case a key bound elsewhere
// in the panel belongs here instead.
func (r reviewScreen) takesEveryKey() bool {
	return r.isWriting() || r.diff.filter.typing
}

// isWriting reports whether the reader is composing a message or asking what to send.
func (r reviewScreen) isWriting() bool {
	return r.threads.composing || r.threads.releasing
}

// isBusy reports whether the reader is part way through something a restart would lose.
func (r reviewScreen) isBusy() bool {
	return r.isWriting() || r.hold.isHeld()
}

// isThreadTaken reports whether the reader has taken the keys onto a conversation the cursor is
// standing on.
func (r reviewScreen) isThreadTaken() bool {
	if !r.threads.focus {
		return false
	}

	_, ok := r.diff.threadAtCursor()

	return ok
}

func (r *reviewScreen) isBlockDrawn(panelShared) bool {
	_, ok := r.diff.threadAtCursor()

	return ok
}

func (r *reviewScreen) isBlockFocused(panelShared) bool {
	return r.isThreadTaken()
}

func (r *reviewScreen) focusBlock(panelShared) {
	r.threads.focus = true
}

func (r *reviewScreen) blurBlock() {
	r.threads.blur()
}

// isThreadFocused is whether the sidebar holds the keys, which it does mid-input and while the
// conversation has been taken.
func (r reviewScreen) isThreadFocused(s panelShared) bool {
	if r.isWriting() {
		return true
	}

	if s.draw.isDetailsFocused() {
		return false
	}

	return r.isThreadTaken()
}

// isDiffFocused is whether the patch holds the keys, which it does whenever the record and the
// conversation beside it do not.
func (r reviewScreen) isDiffFocused(s panelShared) bool {
	return s.draw.isListFocused() && !r.isThreadFocused(s)
}

// update takes the review's own answers and, once nothing in the sidebar has them, the keys.
func (r *reviewScreen) update(h host, s panelShared, msg tea.Msg) tea.Cmd {
	cmd := r.route(h, s, msg)

	return tea.Batch(cmd, r.followWorld(s))
}

func (r *reviewScreen) route(h host, s panelShared, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case reviewLoadedMsg:
		return r.applyLoaded(h, s, msg)
	case reviewPausedMsg:
		return r.applyPaused(h, s, msg)
	case reviewWroteMsg:
		return r.applyWrote(h, s, msg)
	case reviewOffersTracedMsg:
		r.applyOffersTraced(msg)

		return nil
	case messageEditedMsg:
		r.applyMessageEdited(h, s, msg)

		return nil
	case tea.KeyPressMsg:
		return r.updateKey(h, s, msg)
	}

	return nil
}

// enter leaves the review as it stands.
func (r *reviewScreen) enter(host, panelShared, screenTarget) tea.Cmd {
	return nil
}

// enterReview is the agents screen's way onto the review of an agent's diff, with the threads
// in the sidebar and the diff in the stage.
func (m *model) enterReview(agent panel.Agent) tea.Cmd {
	if !isReviewable(agent) {
		return nil
	}

	left := m.enterScreen(screenReview)

	return tea.Batch(left, m.review.open(m.shared(), agent))
}

var (
	_ columned = (*reviewScreen)(nil)
	_ blocked  = (*reviewScreen)(nil)
)

func (r *reviewScreen) doesDrawFromListing() bool {
	return false
}

func (r *reviewScreen) column() columnSpec {
	return reviewColumn()
}
