package ui

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"weave/internal/ui/diffview"
	"weave/internal/ui/paint"
	agentv1 "weave/proto/gen/agent/v1"

	"charm.land/lipgloss/v2"
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

// owedComments are the comments holding the reader's last review, in the order the diff draws
// them.
func (r reviewScreen) owedComments() []owedComment {
	if r.threads.owed == "" || r.diff.view == nil {
		return nil
	}

	owed := make([]owedComment, 0, len(r.threads.list))

	// The thread list is every comment the agent has ever had, so this keeps only the gating
	// review's own.
	for _, thread := range r.threads.list {
		if thread.GetReviewId() != r.threads.owed || thread.GetResolvedAt() != "" {
			continue
		}

		row, drawn := r.diff.view.GetThreadRow(thread.GetId())
		if !drawn {
			row = noRow
		}

		owed = append(owed, owedComment{
			thread: thread,
			row:    row,
			folded: r.diff.view.IsFoldedAway(thread),
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

// isOwedDrawn reports whether the list has anything to say on the screen as it stands.
func (r reviewScreen) isOwedDrawn(s panelShared) bool {
	// The service lifts the gate for a dead agent, and an older round refuses every key offered.
	if _, dead := r.isReadOnly(s); dead {
		return false
	}

	if _, history := r.isHistory(); history {
		return false
	}

	return r.threads.isOwedAnAnswer()
}

// owedView is what the reader has not settled, drawn above the conversation.
func (r reviewScreen) owedView(s panelShared, room int) string {
	if !r.isOwedDrawn(s) {
		return ""
	}

	owed := r.owedComments()
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
		out += r.owedRow(s, comment)
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

// owedRow is one comment, where it stands and what it is waiting for.
func (r reviewScreen) owedRow(s panelShared, comment owedComment) string {
	said := reviewOwedState(comment)
	where := diffview.FormatThreadLocation(comment.thread)

	room := max(lineWidthOf(s.pane)-len(said)-5, 1)
	where = clipToTail(where, room)

	style := dimStyle
	if r.isCommentNear(s, comment) {
		style = paint.Panel.Style(reviewOwedTone).Bold(true)
	}

	pad := strings.Repeat(" ", max(room-lipgloss.Width(where), 0))

	return paint.Panel.Style(reviewHumanTone).Render(" "+diffview.OpenMark+" ") +
		style.Render(where) + dimStyle.Render(pad+"  "+said) + "\n"
}

// clipToTail cuts a path from its front so its line number survives.
func clipToTail(said string, room int) string {
	if lipgloss.Width(said) <= room {
		return said
	}

	return ansi.TruncateLeft(said, lipgloss.Width(said)-room+1, "…")
}

// isCommentNear reports whether a comment's row is one the reader can see from where they are
// standing.
func (r reviewScreen) isCommentNear(s panelShared, comment owedComment) bool {
	if comment.row == noRow {
		return false
	}

	split, ok := s.draw.screenColumnSplit()
	if !ok {
		return comment.row == r.diff.cursor
	}

	return comment.row >= r.diff.top && comment.row < r.diff.top+split.rows
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
