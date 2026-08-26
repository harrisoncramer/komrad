package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/lipgloss"
)

var (
	reviewQuoteStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)

	reviewHumanStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)

	reviewAgentStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)

	reviewNoticeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
)

// reviewView draws the sidebar while the review is up: where the cursor is standing, and the
// conversation pinned there. The diff itself is in the stage, so this side is only ever the
// thread and the line it hangs off.
func (m model) reviewView() string {
	header := m.titleStyle().Render("Review · "+m.review.agentName) + "\n\n"
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
			dimStyle.Render("  ctrl+s save · esc cancel") + "\n"
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
		return dimStyle.Render("  move onto a line to start a thread on it") + "\n"
	}

	out := m.nameStyle(true, true).Render(clipName(reviewWhere(anchor.FilePath, anchor.OldStart, anchor.NewStart))) + "\n"
	for _, quoted := range anchor.QuotedLines {
		out += reviewQuoteStyle.Render(m.reviewWrap(quoted)) + "\n"
	}

	return out
}

func (m model) reviewThreadView() string {
	thread, ok := m.reviewThreadAtCursor()
	if !ok {
		return dimStyle.Render("  no thread here yet") + "\n" + m.reviewCountsView() + m.reviewDriftedView()
	}

	var out strings.Builder

	for _, message := range thread.GetMessages() {
		out.WriteString(m.reviewMessageView(message))
	}

	return out.String() + m.reviewCountsView() + m.reviewDriftedView()
}

// reviewDriftedView lists the threads the cursor can never reach, because the code they were
// written against has changed since. They are not lost and they are not silently re-pinned to
// whatever is at those coordinates now, so the only honest thing left is to say they are there
// and where they used to be.
func (m model) reviewDriftedView() string {
	var drifted []*agentv1.ReviewThread
	for _, thread := range m.review.threads {
		if !m.review.view.IsPinned(thread) {
			drifted = append(drifted, thread)
		}
	}

	if len(drifted) == 0 {
		return ""
	}

	out := "\n" + dimStyle.Render("  moved since they were written") + "\n"
	for _, thread := range drifted {
		out += reviewQuoteStyle.Render("  "+clipName(reviewWhere(thread.GetFilePath(), int(thread.GetOldStart()), int(thread.GetNewStart())))) + "\n"
		if said := firstLine(thread.GetMessages()); said != "" {
			out += m.reviewWrap("    "+said) + "\n"
		}
	}

	return out
}

func firstLine(messages []*agentv1.ReviewMessage) string {
	if len(messages) == 0 {
		return ""
	}

	body := messages[0].GetBody()
	if index := strings.IndexByte(body, '\n'); index >= 0 {
		return body[:index]
	}

	return body
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

// reviewWhere names a place in the diff, on whichever side the line actually lives.
func reviewWhere(filePath string, oldStart, newStart int) string {
	if newStart > 0 {
		return fmt.Sprintf("%s:%d", filePath, newStart)
	}

	return fmt.Sprintf("%s:%d (removed)", filePath, oldStart)
}

func (m model) reviewCountsView() string {
	unread := 0
	for _, thread := range m.review.threads {
		if thread.GetUnreadByAgent() {
			unread++
		}
	}

	counts := fmt.Sprintf("%d thread(s)", len(m.review.threads))
	if unread > 0 {
		counts += fmt.Sprintf(" · %d not yet sent", unread)
	}

	return dimStyle.Render("  "+counts) + "\n"
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

func (m model) reviewHelp() keyMap {
	move := m.moveBinding()
	compose := binding([]string{"c"}, m.reviewComposeDesc())
	threads := binding([]string{"n", "N"}, "next/previous thread")
	files := binding([]string{"]", "["}, "next/previous file")
	page := binding([]string{"pgup", "pgdown"}, "page")
	pan := binding([]string{"h", "l"}, "pan sideways")
	remove := binding([]string{"D"}, "delete the thread")
	send := binding([]string{"s"}, "tell the agent")
	back := binding(m.keys.Agents.Back, "back to the agents")

	return keyMap{
		short: []key.Binding{move, page, compose, threads, m.helpBinding()},
		full: slices.Concat(
			[]key.Binding{move, page, files, threads, pan, compose, remove, send, back},
			m.globalHelp(),
		),
	}
}
