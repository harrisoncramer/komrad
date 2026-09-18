package ui

import (
	"fmt"
	"slices"
	"strings"

	"weave/internal/review"
	"weave/internal/ui/paint"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Explainer: the sidebar draws the comment under the cursor and nothing else, so what a reader
// is still holding could only be learned by walking the whole diff. The strip is that fact drawn
// once, and it is scoped to the review the service reads rather than to every thread the agent
// has, since two denominators on one screen is what let a panel say a review was owed an answer
// while the key it named reported everything settled.

// reviewOwedTone is what the strip's title and the comments in view are drawn in.
const reviewOwedTone = paint.ToneAccent

// stripRows is the most comments the strip names before it counts the rest.
const stripRows = 3

// stripThreadRows is what the conversation under the strip is kept, the strip giving up its rows
// before that does.
const stripThreadRows = 3

// owedComment is one comment holding the reader's last review, and where it stands now.
type owedComment struct {
	thread *agentv1.ReviewThread
	// row is where the diff draws it, negative for one no row carries.
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

	for _, thread := range m.review.threads {
		if thread.GetReviewId() != m.review.owed || thread.GetResolvedAt() != "" {
			continue
		}

		row, drawn := m.review.view.RowOfThread(thread.GetId())
		if !drawn {
			row = -1
		}

		owed = append(owed, owedComment{
			thread: thread,
			row:    row,
			folded: m.review.view.IsFoldedAway(thread),
		})
	}

	slices.SortFunc(owed, func(a, b owedComment) int {
		if a.row != b.row {
			return cmp(a.row, b.row)
		}

		return strings.Compare(a.thread.GetId(), b.thread.GetId())
	})

	return owed
}

// cmp orders two rows, putting a comment with no row after every comment that has one.
func cmp(a, b int) int {
	switch {
	case a < 0:
		return 1
	case b < 0:
		return -1
	case a < b:
		return -1
	}

	return 1
}

// isReviewStripDrawn reports whether the strip has anything to say on the screen as it stands.
func (m model) isReviewStripDrawn() bool {
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

// reviewStripView is what the reader has not settled, drawn above the conversation and sized to
// what the sidebar can spare.
func (m model) reviewStripView(room int) string {
	if !m.isReviewStripDrawn() {
		return ""
	}

	owed := m.reviewOwedComments()
	if len(owed) == 0 {
		return ""
	}

	palette := m.palette(regionThread)
	out := palette.Style(reviewOwedTone).Bold(true).Render("Open in your review") +
		dimStyle.Render(fmt.Sprintf("  %d", len(owed))) + "\n"

	if room <= 0 {
		return out
	}

	shown := min(room, len(owed))
	for _, comment := range owed[:shown] {
		out += m.reviewStripRow(comment)
	}

	if left := len(owed) - shown; left > 0 {
		out += dimStyle.Render(fmt.Sprintf(" + %d more", left)) + "\n"
	}

	return out
}

// reviewStripRoom is how many comments the strip may name once the sidebar's own chrome has
// taken its share.
func reviewStripRoom(spare int) int {
	// The title row, the line the cursor is standing on, and the conversation's own keep.
	return min(max(spare-2-stripThreadRows, 0), stripRows)
}

// reviewStripRow is one comment: where it stands, and the one word that says what it is waiting
// for.
func (m model) reviewStripRow(comment owedComment) string {
	said := reviewOwedState(comment)
	where := review.FormatThreadLocation(comment.thread)

	room := max(m.lineWidth()-len(said)-5, 1)
	where = clipToTail(where, room)

	style := dimStyle
	if m.isReviewCommentNear(comment) {
		style = m.palette(regionThread).Style(reviewOwedTone).Bold(true)
	}

	pad := strings.Repeat(" ", max(room-lipgloss.Width(where), 0))

	return m.palette(regionThread).Style(reviewHumanTone).Render(" "+review.OpenMark+" ") +
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
	if comment.row < 0 {
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
