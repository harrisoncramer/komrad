package ui

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"weave/internal/review"
	"weave/internal/ui/paint"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// reviewOwedTone is what the title and the comments in view are drawn in.
const reviewOwedTone = paint.ToneAccent

// owedRows is the most comments named before the rest are counted.
const owedRows = 3

// owedThreadRows is what the conversation below is kept.
const owedThreadRows = 3

// noRow is the place of a comment the diff draws nowhere, which sorts last.
const noRow = math.MaxInt

// owedComment is one comment holding the reader's last review, and where it stands now.
type owedComment struct {
	thread *agentv1.ReviewThread
	// row is where the diff draws it, noRow for one no row carries.
	row int
	// folded says its file has been read and put away, which is why it has no row.
	folded bool
}

// reviewOwedComments are the comments holding the reader's last review, in the order the diff
// draws them.
func (m model) reviewOwedComments() []owedComment {
	if m.review.owed == "" || m.review.view == nil {
		return nil
	}

	owed := make([]owedComment, 0, len(m.review.threads))

	// The whole thread list is held unfiltered, so this reads the gating review's own comments
	// rather than every comment the agent has ever had.
	for _, thread := range m.review.threads {
		if thread.GetReviewId() != m.review.owed || thread.GetResolvedAt() != "" {
			continue
		}

		row, drawn := m.review.view.RowOfThread(thread.GetId())
		if !drawn {
			row = noRow
		}

		owed = append(owed, owedComment{
			thread: thread,
			row:    row,
			folded: m.review.view.IsFoldedAway(thread),
		})
	}

	slices.SortFunc(owed, func(a, b owedComment) int {
		return cmp.Or(cmp.Compare(a.row, b.row), strings.Compare(a.thread.GetId(), b.thread.GetId()))
	})

	return owed
}

// reviewOwedSaid is how many comments of the reader's are still open, which is what every
// refusal leads with.
func reviewOwedSaid(owed []owedComment) string {
	// The gate is read off the service and the rows off the conversation, so the sentence has to
	// stand up in the moment before the comments themselves have arrived.
	if len(owed) == 0 {
		return "your review is still owed an answer"
	}

	if len(owed) == 1 {
		return "1 comment of yours is still open"
	}

	return fmt.Sprintf("%d comments of yours are still open", len(owed))
}

// isReviewOwedDrawn reports whether the strip has anything to say on the screen as it stands.
func (m model) isReviewOwedDrawn() bool {
	// A dead agent has the gate lifted for it by the service, and an older round refuses every
	// key the strip offers, so a strip naming what is held would be false on both.
	if _, dead := m.isReviewReadOnly(); dead {
		return false
	}

	if _, history := m.isReviewHistory(); history {
		return false
	}

	return m.review.isOwedAnAnswer()
}

// reviewOwedView is what the reader has not settled, drawn above the conversation and sized to
// what the sidebar can spare.
func (m model) reviewOwedView(room int) string {
	if !m.isReviewOwedDrawn() {
		return ""
	}

	owed := m.reviewOwedComments()
	if len(owed) == 0 {
		return ""
	}

	out := paint.Panel.Style(reviewOwedTone).Bold(true).Render("Open in your review") +
		dimStyle.Render(fmt.Sprintf("  %d", len(owed))) + "\n"

	if room <= 0 {
		return out
	}

	shown := min(room, len(owed))
	for _, comment := range owed[:shown] {
		out += m.reviewOwedRow(comment)
	}

	if left := len(owed) - shown; left > 0 {
		out += dimStyle.Render(fmt.Sprintf(" + %d more", left)) + "\n"
	}

	return out
}

// reviewOwedRoom is how many comments the strip may name once the sidebar's own chrome has
// taken its share.
func reviewOwedRoom(spare int) int {
	// The title row, the line the cursor is standing on, and the conversation's own keep.
	return min(max(spare-2-owedThreadRows, 0), owedRows)
}

// reviewOwedRow is one comment: where it stands, and the one word that says what it is waiting
// for.
func (m model) reviewOwedRow(comment owedComment) string {
	said := reviewOwedState(comment)
	where := review.FormatThreadLocation(comment.thread)

	room := max(m.lineWidth()-len(said)-5, 1)
	where = clipToTail(where, room)

	style := dimStyle
	if m.isReviewCommentNear(comment) {
		style = paint.Panel.Style(reviewOwedTone).Bold(true)
	}

	pad := strings.Repeat(" ", max(room-lipgloss.Width(where), 0))

	return paint.Panel.Style(reviewHumanTone).Render(" "+review.OpenMark+" ") +
		style.Render(where) + dimStyle.Render(pad+"  "+said) + "\n"
}

// clipToTail cuts a path from its front rather than its back, since the line number a comment
// stands on is the end of it.
func clipToTail(said string, room int) string {
	if lipgloss.Width(said) <= room {
		return said
	}

	return ansi.TruncateLeft(said, lipgloss.Width(said)-room+1, "…")
}

// isReviewCommentNear reports whether a comment's row is one the reader can see from where they
// are standing.
func (m model) isReviewCommentNear(comment owedComment) bool {
	if comment.row == noRow {
		return false
	}

	split, ok := m.screenColumnSplit()
	if !ok {
		return comment.row == m.review.cursor
	}

	return comment.row >= m.review.top && comment.row < m.review.top+split.rows
}

// reviewOwedState is the one word a strip row carries, which is what the comment is waiting for
// rather than what it says.
func reviewOwedState(comment owedComment) string {
	if comment.folded {
		return "folded away"
	}

	messages := comment.thread.GetMessages()
	for _, message := range messages {
		if message.GetDelivery() == agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT {
			return "reply unsent"
		}
	}

	for _, message := range messages[min(1, len(messages)):] {
		if message.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
			return "answered"
		}
	}

	return "no reply"
}
