package ui

import (
	"fmt"
	"slices"
	"time"

	"weave/internal/review"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

var (
	reviewQuoteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)

	reviewHumanStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)

	reviewAgentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)

	reviewNoticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

	reviewAddedStyle = lipgloss.NewStyle().Foreground(colorSuccess)

	reviewRemovedStyle = lipgloss.NewStyle().Foreground(colorDanger)
)

// reviewView draws the sidebar while the review is up: where the cursor is standing, and the
// conversation pinned there. The diff itself is in the stage, so this side is only ever the
// thread and the line it hangs off.
func (m model) reviewView() string {
	header := m.titleStyle().Render("Review · "+m.review.agentName) + "\n" +
		m.reviewStatView() +
		dimStyle.Render("Conversation state: "+m.reviewConversationState()) + "\n\n" +
		m.review.filter.view() + m.reviewMatchCount()
	help := m.renderHelp(m.reviewHelp())

	// The head is pinned and the conversation gives up the room: where the cursor is standing is
	// what makes the rest of the pane mean anything.
	head := m.reviewHeadView()
	rest := tailRows(m.reviewRestView(), m.listLines(header, help, head))

	return header + padRows(head+rest, m.listLines(header, help)) + help
}

// reviewHeadView is the fixed top of the sidebar: whatever is standing in the way of a review,
// or the place the cursor is standing when nothing is.
func (m model) reviewHeadView() string {
	switch {
	case m.review.err != nil:
		return brokenStyle.Render(m.review.err.Error()) + "\n"
	case m.review.loading:
		return dimStyle.Render("loading the diff…") + "\n"
	case m.review.view == nil || m.review.view.Len() == 0:
		return dimStyle.Render("nothing changed in this worktree") + "\n"
	}

	return m.reviewAnchorView()
}

func (m model) reviewRestView() string {
	if m.review.err != nil || m.review.loading || m.review.view == nil || m.review.view.Len() == 0 {
		return ""
	}

	if m.review.composing {
		return "\n" + m.review.editor.View() + "\n" +
			dimStyle.Render("  "+keyLabel(m.keys.Form.Submit)+" save · esc cancel") + "\n"
	}

	out := "\n" + m.reviewThreadView()
	if notice := m.review.notice; notice != "" {
		out += "\n" + reviewNoticeStyle.Render("  "+notice) + "\n"
	}

	return out
}

// reviewAnchorView is the place the cursor is standing, and the line it is standing on, so the
// sidebar says what a new thread would be pinned to.
func (m model) reviewAnchorView() string {
	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
	if !ok {
		if thread, ok := m.reviewThreadAtCursor(); ok {
			return m.nameStyle(true, true).Render(clipName(review.WhereOf(thread))) + "\n"
		}

		return dimStyle.Render("  move onto a line to start a thread on it") + "\n"
	}

	out := m.nameStyle(true, true).Render(clipName(review.Where(anchor.FilePath, anchor.OldStart, anchor.NewStart))) + "\n"
	for _, quoted := range anchor.QuotedLines {
		out += reviewQuoteStyle.Render(m.reviewWrap(quoted)) + "\n"
	}

	return out
}

// reviewStatView is how big the change is, drawn above the conversation so the first thing the
// sidebar says about a diff is its size. The counts are coloured the way the lines themselves
// are, so which number is which is read rather than parsed.
func (m model) reviewStatView() string {
	if m.review.view == nil {
		return ""
	}

	stat := m.review.view.Stat()
	if stat.Files == 0 {
		return ""
	}

	files := fmt.Sprintf("%d file", stat.Files)
	if stat.Files != 1 {
		files += "s"
	}

	return dimStyle.Render(files+" changed") + "  " +
		reviewAddedStyle.Render(fmt.Sprintf("+%d", stat.Added)) + " " +
		reviewRemovedStyle.Render(fmt.Sprintf("-%d", stat.Removed)) + "\n"
}

// reviewMatchCount is where in the search the cursor is standing, drawn under the pattern the
// panel's own filter is already showing.
func (m model) reviewMatchCount() string {
	if m.review.view == nil || !m.review.filter.standing() {
		return ""
	}

	where := matchingLines(m.review.view.MatchCount())
	if at, ok := m.review.view.MatchNumber(m.review.cursor); ok {
		where = fmt.Sprintf("%d of %d", at, m.review.view.MatchCount())
	}

	return dimStyle.Render("  "+where) + "\n\n"
}

func matchingLines(count int) string {
	if count == 1 {
		return "1 matching line"
	}

	return fmt.Sprintf("%d matching lines", count)
}

func (m model) reviewThreadView() string {
	thread, ok := m.reviewThreadAtCursor()
	if !ok {
		return dimStyle.Render("  no thread here yet") + "\n"
	}

	out := m.reviewOriginView(thread)
	for _, message := range thread.GetMessages() {
		out += m.reviewMessageView(message)
	}

	return out
}

// reviewOriginView is where a thread was written and the line it was written against, carried
// with it so it still says what it was about once that code has changed.
//
// It is drawn only for a thread that has moved. Where the thread is still standing on its own
// line, the diff beside the sidebar is already showing it, and saying it twice is noise.
func (m model) reviewOriginView(thread *agentv1.ReviewThread) string {
	if !m.review.view.HasMoved(thread) {
		return ""
	}

	out := dimStyle.Render("  written against "+clipName(review.WhereOf(thread))) + "\n"
	for _, quoted := range thread.GetQuotedLines() {
		out += reviewQuoteStyle.Render(m.reviewWrap("  "+quoted)) + "\n"
	}

	return out + "\n"
}

func (m model) reviewMessageView(message *agentv1.ReviewMessage) string {
	author, style := "you", reviewHumanStyle
	if message.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
		author, style = m.review.agentName, reviewAgentStyle
	}

	head := style.Render(author)
	if when := reviewTimeOf(message.GetCreatedAt()); when != "" {
		head += " " + dimStyle.Render(when)
	}

	return head + "\n" + m.reviewWrap(message.GetBody()) + "\n\n"
}

// reviewConversationState is whose turn it is, in one phrase at the top of the sidebar. Whether
// the agent has read the review, whether it has been told to, and whether it has answered are
// all the same question asked from different sides, so they are answered once and in one place.
//
// The order is what to do about it: an answer to read comes before work to send, and work to
// send before waiting on somebody else to do it.
func (m model) reviewConversationState() string {
	switch {
	case len(m.review.threads) == 0:
		return "nothing said yet"
	case m.reviewUnreadByHuman():
		return "the agent has replied"
	case m.reviewAllSettled():
		return "everything is settled"
	case !m.reviewUnreadByAgent():
		return "the agent is up to date"
	case m.reviewAgentHasMail():
		return "waiting on the agent"
	}

	return "not sent yet"
}

// reviewAllSettled is whether every thread has been finished with, which is the difference
// between an agent that is up to date and a review that is done.
func (m model) reviewAllSettled() bool {
	return !slices.ContainsFunc(m.review.threads, func(thread *agentv1.ReviewThread) bool {
		return thread.GetResolvedAt() == ""
	})
}

func (m model) reviewUnreadByHuman() bool {
	return slices.ContainsFunc(m.review.threads, func(thread *agentv1.ReviewThread) bool {
		return thread.GetUnreadByHuman()
	})
}

func (m model) reviewUnreadByAgent() bool {
	return slices.ContainsFunc(m.review.threads, func(thread *agentv1.ReviewThread) bool {
		return thread.GetUnreadByAgent()
	})
}

// reviewAgentHasMail reads the doorbell off the agents list rather than the review, since that
// is the one thing on this screen that refreshes on its own: a notice the agent picks up stops
// being drawn without a keypress.
func (m model) reviewAgentHasMail() bool {
	return slices.ContainsFunc(m.listing.agents, func(item agentItem) bool {
		return item.agent.ID == m.review.agentID && item.unreadMail
	})
}

func (m model) reviewWrap(body string) string {
	return lipgloss.NewStyle().Width(max(20, m.width-2)).Render(body)
}

func reviewTimeOf(stamp string) string {
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return ""
	}

	return at.Local().Format("3:04PM")
}

// reviewComposeDesc says what the compose key would do where the cursor is standing, since it
// starts a thread on a line that has none and answers the one that is already there.
func (m model) reviewComposeDesc() string {
	if _, ok := m.reviewThreadAtCursor(); ok {
		return "reply in this thread"
	}

	return "start a thread here"
}

// reviewResolveDesc says which way the settle key would go where the cursor is standing.
func (m model) reviewResolveDesc() string {
	if thread, ok := m.reviewThreadAtCursor(); ok && thread.GetResolvedAt() != "" {
		return "reopen the thread"
	}

	return "settle the thread"
}

func (m model) reviewHelp() keyMap {
	move := m.moveBinding()
	compose := binding([]string{"c"}, m.reviewComposeDesc())

	// Only the three things the screen is for are on the strip. Reviewing is reading, and a
	// legend of ten keys is in the way of that; the rest are a ? away.
	return keyMap{
		short: []key.Binding{move, compose, m.helpBinding()},
		full: slices.Concat(
			[]key.Binding{
				move,
				compose,
				binding([]string{"ctrl+n", "ctrl+p", "pgdown", "pgup"}, "page"),
				binding([]string{"]", "["}, "next/previous file"),
				binding([]string{"}", "{"}, "next/previous hunk"),
				binding([]string{"t", "T"}, "next/previous thread"),
				binding(m.keys.Filter.Open, "search"),
				binding([]string{"n", "N"}, "next/previous match"),
				binding([]string{"zz", "zt", "zb"}, "recentre the view"),
				binding([]string{"h", "l"}, "pan sideways"),
				binding([]string{"e"}, m.reviewEditorDesc()),
				binding([]string{"r"}, m.reviewResolveDesc()),
				binding([]string{"D"}, "delete the thread"),
				binding(m.keys.Form.Submit, "tell the agent"),
				binding(m.keys.Agents.Back, "back to the agents"),
			},
			m.globalHelp(),
		),
	}
}
