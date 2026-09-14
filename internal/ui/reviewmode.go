package ui

import (
	"fmt"

	"weave/internal/ui/paint"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
)

// The mode is the one thing on the screen that changes what every key does, so each state gets
// its own colour.
var (
	reviewWatchingStyle = lipgloss.NewStyle().Foreground(paint.Accent).Bold(true)
	reviewEditingStyle  = lipgloss.NewStyle().Foreground(paint.Success).Bold(true)
	reviewWritingStyle  = lipgloss.NewStyle().Foreground(paint.Mark).Bold(true)
	reviewHistoryStyle  = lipgloss.NewStyle().Foreground(paint.Warning).Bold(true)
	reviewHeldStyle     = lipgloss.NewStyle().Foreground(paint.Muted).Bold(true)
)

// reviewMode is which of the three things the review screen is doing.
type reviewMode uint8

const (
	// modeWatching is the mode the screen opens in, where the diff keeps up with the agent and
	// nothing a key does reaches it.
	modeWatching reviewMode = iota
	// modeEditing is your own editor holding the stage over the agent's worktree.
	modeEditing
	// modeReviewing is a round frozen so a batch of comments can be pinned to it.
	modeReviewing
)

// reviewModeView says which of the three the screen is in, and what to press next.
func (m model) reviewModeView() string {
	switch m.review.mode {
	case modeEditing:
		return m.reviewBandRow(reviewEditingStyle, "editing", m.reviewEditingSaid())
	case modeReviewing:
		return m.reviewBandRow(reviewWritingStyle, "reviewing", m.reviewWritingSaid())
	case modeWatching:
	}

	return m.reviewWatchingView()
}

// reviewWatchingView is the mode line while nothing is being written.
func (m model) reviewWatchingView() string {
	if _, dead := m.isReviewReadOnly(); dead {
		return m.reviewBandRow(reviewHeldStyle, "watching", "this agent is gone, nothing can be sent")
	}

	if said, history := m.isReviewHistory(); history {
		return m.reviewBandRow(reviewHistoryStyle, "reading history",
			said+" is finished · "+keyLabel(m.keys.Review.BaseForward)+" for the current work")
	}

	if m.doesReviewHoldUnsent() {
		return m.reviewBandRow(reviewHeldStyle, "watching",
			counted(m.reviewUnsentCount(), "comment")+" not sent · "+
				keyLabel(m.keys.Review.Compose)+" to carry on")
	}

	if m.review.awaiting {
		return m.reviewBandRow(reviewHeldStyle, "watching",
			"your review is owed an answer · "+keyLabel(m.keys.Review.NextOpenThread)+
				" for the comments left open")
	}

	return m.reviewBandRow(reviewWatchingStyle, "watching",
		"live · "+keyLabel(m.keys.Review.EditFile)+" to edit · "+
			keyLabel(m.keys.Review.Compose)+" to comment")
}

// reviewEditingSaid is what the editing mode offers: the file being edited, and the way back to
// the diff.
func (m model) reviewEditingSaid() string {
	said := "the agent's own worktree"
	if m.review.editingPath != "" {
		said = m.review.editingPath
	}

	return said + " · " + keyLabel(m.keys.Review.EditFile) + " to come back"
}

// reviewWritingSaid is what the reviewing mode offers: the way out until something has been
// written, and the way to send it once something has.
func (m model) reviewWritingSaid() string {
	if !m.doesReviewHoldUnsent() {
		return "nothing written yet · " + keyLabel(m.keys.Review.Back) + " to go back to watching"
	}

	if m.review.pinned {
		return "the code as it was when you started · " + keyLabel(formKeys().Submit) + " sends it"
	}

	return keyLabel(formKeys().Submit) + " sends it to the agent"
}

// reviewUnsentCount is how many comments the reader is holding.
func (m model) reviewUnsentCount() int {
	return m.review.activity.delivery[deliverySlot(agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT)]
}

// isReviewReadOnly reports whether there is nobody there to receive a comment.
func (m model) isReviewReadOnly() (string, bool) {
	for _, item := range m.listing.agents {
		if item.agent.ID != m.review.agentID {
			continue
		}

		if item.alive {
			return "", false
		}

		return "this agent is dormant · resume it before writing to it", true
	}

	return "this agent is archived · restore it before writing to it", true
}

// doesReviewHoldUnsent says whether the reader has comments nobody has seen.
func (m model) doesReviewHoldUnsent() bool {
	return m.reviewUnsentCount() > 0
}

// isReviewHistory reports whether the round on screen ends before the agent's current work.
func (m model) isReviewHistory() (string, bool) {
	// Explainer: what decides this is the snapshot the diff was drawn to, not what the chapter at
	// the reader's index happens to say now. The list is recut on every load, so an index and a
	// list can disagree, and the refusal has to follow the code on screen rather than the label.
	drawn := m.review.head
	if m.review.view != nil {
		drawn = m.review.view.GetSnapshotSHA()
	}

	if drawn == "" || drawn == m.review.round.GetSnapshotSha() {
		return "", false
	}

	at := m.review.chapter
	if at < 0 || at >= len(m.review.chapters) {
		return "an earlier round", true
	}

	return fmt.Sprintf("round %d of %d", at+1, len(m.review.chapters)), true
}

// doesReviewWalk says whether the round keys have anywhere to go, which watching has and the two
// modes that hold the tree do not.
func (m model) doesReviewWalk() bool {
	return m.review.mode == modeWatching && len(m.review.chapters) > 0
}
