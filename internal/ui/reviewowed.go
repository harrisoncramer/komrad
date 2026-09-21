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

// owedThreadRows is how many rows the conversation below keeps.
const owedThreadRows = 3

// noRow is the place of a comment the diff draws nowhere, which sorts last.
const noRow = math.MaxInt

// owedComment is one comment holding the reader's last review, and where it stands now.
type owedComment struct {
	thread *agentv1.ReviewThread
	// row is where the diff draws it, noRow for one no row carries.
	row int
	// folded says its file has been read and put away.
	folded bool
}

// reviewOwedComments are the comments holding the reader's last review, in the order the diff
// draws them.
func (m model) reviewOwedComments() []owedComment {
	if m.review.owed == "" || m.review.diff.view == nil {
		return nil
	}

	owed := make([]owedComment, 0, len(m.review.threads))

	// The thread list is every comment the agent has ever had, so this keeps only the gating
	// review's own.
	for _, thread := range m.review.threads {
		if thread.GetReviewId() != m.review.owed || thread.GetResolvedAt() != "" {
			continue
		}

		row, drawn := m.review.diff.view.GetThreadRow(thread.GetId())
		if !drawn {
			row = noRow
		}

		owed = append(owed, owedComment{
			thread: thread,
			row:    row,
			folded: m.review.diff.view.IsFoldedAway(thread),
		})
	}

	slices.SortFunc(owed, func(a, b owedComment) int {
		return cmp.Or(cmp.Compare(a.row, b.row), strings.Compare(a.thread.GetId(), b.thread.GetId()))
	})

	return owed
}

// reviewOwedSaid is how many comments of the reader's are still open.
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

// isReviewOwedDrawn reports whether the list has anything to say on the screen as it stands.
func (m model) isReviewOwedDrawn() bool {
	// The service lifts the gate for a dead agent, and an older round refuses every key offered.
	if _, dead := m.isReviewReadOnly(); dead {
		return false
	}

	if _, history := m.review.isHistory(); history {
		return false
	}

	return m.review.isOwedAnAnswer()
}

// reviewOwedView is what the reader has not settled, drawn above the conversation.
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

// reviewOwedRoom is how many comments fit once the sidebar's own chrome has taken its share.
func reviewOwedRoom(spare int) int {
	// The title row, the line the cursor is standing on, and the conversation's own keep.
	return min(max(spare-2-owedThreadRows, 0), owedRows)
}

// reviewOwedRow is one comment, where it stands and what it is waiting for.
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

// clipToTail cuts a path from its front so its line number survives.
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
		return comment.row == m.review.diff.cursor
	}

	return comment.row >= m.review.diff.top && comment.row < m.review.diff.top+split.rows
}

// reviewOwedState is the one word a row carries about what the comment is waiting for.
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
