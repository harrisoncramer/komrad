package ui

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"

	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// reviewYoursTone is what the title and the comments in view are drawn in.
const reviewYoursTone = paint.ToneAccent

// yoursRows is the most comments named before the rest are counted.
const yoursRows = 3

// yoursThreadRows is how many rows the conversation below keeps.
const yoursThreadRows = 3

// noRow is the place of a comment the diff draws nowhere, which sorts last.
const noRow = math.MaxInt

// yourComment is one of the reader's comments, and where it stands now.
type yourComment struct {
	thread *agentv1.ReviewThread
	// row is where the diff draws it, noRow for one no row carries.
	row int
	// folded says its file has been read and put away.
	folded bool
}

// listYourComments lists the comments the reader started, the ones with something unread first, then
// the open ones, each group in the order the diff draws them.
func (r reviewScreen) listYourComments() []yourComment {
	if r.diff.view == nil {
		return nil
	}

	yours := make([]yourComment, 0, len(r.threads.list))
	for _, thread := range r.threads.list {
		if !isYours(thread) {
			continue
		}

		row, drawn := r.diff.view.GetThreadRow(thread.GetId())
		if !drawn {
			row = noRow
		}

		yours = append(yours, yourComment{
			thread: thread,
			row:    row,
			folded: r.diff.view.IsFoldedAway(thread),
		})
	}

	slices.SortFunc(yours, func(a, b yourComment) int {
		return cmp.Or(
			compareFirst(a.thread.GetUnreadByHuman(), b.thread.GetUnreadByHuman()),
			compareFirst(a.thread.GetResolvedAt() == "", b.thread.GetResolvedAt() == ""),
			cmp.Compare(a.row, b.row),
			strings.Compare(a.thread.GetId(), b.thread.GetId()),
		)
	})

	return yours
}

// isYours reports whether the reader is the one who started a thread.
func isYours(thread *agentv1.ReviewThread) bool {
	messages := thread.GetMessages()

	return len(messages) > 0 && messages[0].GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_HUMAN
}

// compareFirst orders true before false.
func compareFirst(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return -1
	}

	return 1
}

// isYoursDrawn reports whether the list has anything to say on the screen as it stands.
func (r reviewScreen) isYoursDrawn() bool {
	// An older round draws none of the rows the list points at.
	_, history := r.isHistory()

	return !history
}

// yoursView is every comment the reader started, drawn above the conversation.
func (r reviewScreen) yoursView(s panelShared, room int) string {
	if room <= 0 || !r.isYoursDrawn() {
		return ""
	}

	yours := r.listYourComments()
	if len(yours) == 0 {
		return ""
	}

	open := 0
	for _, comment := range yours {
		if comment.thread.GetResolvedAt() == "" {
			open++
		}
	}

	out := paint.Panel.Style(reviewYoursTone).Bold(true).Render("Yours") +
		dimStyle.Render(fmt.Sprintf("  %d open, %d resolved", open, len(yours)-open)) + "\n"

	shown := min(room, len(yours))
	for _, comment := range yours[:shown] {
		out += r.yoursRow(s, comment)
	}

	if left := len(yours) - shown; left > 0 {
		out += dimStyle.Render(fmt.Sprintf(" + %d more", left)) + "\n"
	}

	return out
}

// countYoursRoom counts how many comments fit once the sidebar's own chrome has taken its share.
func countYoursRoom(spare int) int {
	// The title row, the line the cursor is standing on, and the conversation's own keep.
	return min(max(spare-2-yoursThreadRows, 0), yoursRows)
}

// yoursRow is one comment, where it stands and what it is waiting for.
func (r reviewScreen) yoursRow(s panelShared, comment yourComment) string {
	said := describeYoursState(comment)
	where := diffview.FormatThreadLocation(comment.thread)

	room := max(lineWidthOf(s.pane)-len(said)-5, 1)
	where = clipToTail(where, room)

	style := dimStyle
	if r.isCommentNear(s, comment) {
		style = paint.Panel.Style(reviewYoursTone).Bold(true)
	}

	pad := strings.Repeat(" ", max(room-lipgloss.Width(where), 0))

	mark := diffview.OpenMark
	if comment.thread.GetResolvedAt() != "" {
		mark = diffview.SettledMark
	}

	return paint.Panel.Style(reviewHumanTone).Render(" "+mark+" ") +
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
func (r reviewScreen) isCommentNear(s panelShared, comment yourComment) bool {
	if comment.row == noRow {
		return false
	}

	split, ok := s.draw.screenColumnSplit()
	if !ok {
		return comment.row == r.diff.cursor
	}

	return comment.row >= r.diff.top && comment.row < r.diff.top+split.rows
}

// describeYoursState gives the few words a row carries about what the comment is waiting for.
func describeYoursState(comment yourComment) string {
	if comment.thread.GetUnreadByHuman() {
		return "unread"
	}

	if comment.folded {
		return "folded away"
	}

	messages := comment.thread.GetMessages()
	for _, message := range messages {
		if message.GetDelivery() == agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT {
			return "unsent"
		}
	}

	if comment.thread.GetResolvedAt() != "" {
		return "resolved"
	}

	if len(messages) > 1 && messages[len(messages)-1].GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
		return "answered"
	}

	return "no reply"
}
