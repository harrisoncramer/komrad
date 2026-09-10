package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/internal/review"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	reviewQuoteStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	reviewHumanStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	reviewAgentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	reviewNoticeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	reviewAddedStyle   = lipgloss.NewStyle().Foreground(colorSuccess)
	reviewRemovedStyle = lipgloss.NewStyle().Foreground(colorDanger)
	// reviewMovedStyle draws the catch-up notice as coloured text rather than as a filled bar, the
	// agent writing while you read being ordinary rather than an alarm.
	reviewMovedStyle = lipgloss.NewStyle().Foreground(colorWarning)

	// Reversed rather than merely coloured: a reader who keeps writing against a patch that has
	// already moved has those comments pinned as ghosted or adrift when they finally reload, so
	// this is worth a bar across the sidebar rather than a dim line they read past.
	reviewDeadStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(colorMuted).Bold(true)
	reviewUnsentStyle    = lipgloss.NewStyle().Foreground(colorAccent)
	reviewSentStyle      = lipgloss.NewStyle().Foreground(colorWarning)
	reviewDeliveredStyle = lipgloss.NewStyle().Foreground(colorSuccess)
)

// reviewView draws the diff and, beside it, the thread the cursor is standing on, as one frame.
func (m model) reviewView() string {
	split, ok := m.screenColumnSplit()
	if !ok {
		band := m.reviewBandView(m.paneWidth())

		return band + strings.TrimRight(m.reviewSidebarView(band), "\n")
	}

	// Trimmed so the two halves are the same height.
	sidebar := strings.TrimRight(m.reviewSidebarView(), "\n")

	// The band stands over the diff rather than over the whole window, and what it carries acts on
	// the patch.
	diff := m.reviewBandView(split.column) + m.reviewDiffView(split)

	return m.joinColumns(split, sidebar, diff)
}

// reviewSidebarView is the panel's own column: where the cursor is standing, and the thread
// hanging off it.
func (m model) reviewSidebarView(above ...string) string {
	help := m.renderHelp(m.reviewHelp())

	// The head is pinned and the conversation gives up the room: where the cursor is standing is
	// what makes the rest of the pane mean anything.
	head := m.reviewHeadView()
	record := m.detailsView()
	messages := m.messagesRegion(record, help)
	chrome := slices.Concat(above, []string{help, record, messages})
	rest := m.reviewThreadWindow(m.reviewRestView(), m.listLines(slices.Concat(chrome, []string{head})...))

	return padRows(head+rest, m.listLines(chrome...)) + record + messages + help
}

// reviewThreadWindow cuts the conversation down to the rows the sidebar has, keeping its end
// unless the reader has taken the keys and scrolled up into it.
func (m model) reviewThreadWindow(rendered string, lines int) string {
	if lines <= 0 {
		return ""
	}

	rows := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(rows) <= lines {
		return rendered
	}

	// Placed from the end of the conversation rather than around a cursor, so the newest message
	// holds its row as the thread grows and the reader walks back from there.
	place := func(visible int) int {
		return len(rows) - visible - min(m.review.threadScroll, len(rows)-visible)
	}

	return windowRows(rows, lines, place, m.reviewMarkerStyle())
}

// doesReviewFitDetails reports whether the sidebar has a line left over once the record has taken
// its share.
func (m model) doesReviewFitDetails() bool {
	// Measured with the keys on the diff, which keeps this out of its own answer.
	m.details.focused = false

	spare := m.spareLines(m.renderHelp(m.reviewHelp()), m.reviewHeadView())

	return spare > m.detailsLines()+detailsChromeLines
}

// reviewHeadView is the fixed top of the sidebar: whatever is standing in the way of a review, or
// the place the cursor is standing when nothing is.
func (m model) reviewHeadView() string {
	switch {
	case m.review.loading:
		return dimStyle.Render(reviewLoadingWord) + "\n"
	case m.review.view == nil || m.review.view.Len() == 0:
		return dimStyle.Render(reviewEmptyWord) + "\n"
	}

	return m.reviewAnchorView()
}

// reviewNote is a line of prose under the conversation, wrapped into the sidebar's own column.
func (m model) reviewNote(style lipgloss.Style, text string) string {
	width := max(20, m.paneWidth()-2)

	var out strings.Builder
	for _, row := range strings.Split(ansi.Wordwrap(text, width, ""), "\n") {
		out.WriteString(style.Render("  "+row) + "\n")
	}

	return out.String()
}

func (m model) reviewRestView() string {
	if m.review.loading || m.review.view == nil || m.review.view.Len() == 0 {
		return ""
	}

	if m.review.composing {
		return "\n" + m.review.editor.View() + "\n" +
			dimStyle.Render("  "+keyLabel(formKeys().Submit)+" save · "+
				keyLabel(formKeys().Editor)+" editor · "+keyLabel(formKeys().Back)+" cancel") + "\n"
	}

	if m.review.releasing {
		return m.reviewReleaseView()
	}

	out := "\n" + m.reviewThreadView()

	// A notice answers the last keypress and a keypress clears it, so what the reader is told
	// about the diff itself stands underneath and comes back the moment the answer is gone.
	if notice := m.review.notice; notice != "" {
		out += "\n" + m.reviewNote(reviewNoticeStyle, notice)
	} else if since := m.review.since; since != "" && m.review.baseLabel == "" {
		out += "\n" + m.reviewNote(dimStyle, since)
	}

	return out
}

// reviewAnchorView names the place the cursor is standing, so the sidebar says what a new thread
// would be pinned to without repeating the line the diff already draws beside it.
func (m model) reviewAnchorView() string {
	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
	if !ok {
		if thread, ok := m.reviewThreadAtCursor(); ok {
			return titleOf(m.isReviewThreadFocused(), clipName(review.FormatThreadLocation(thread))) +
				m.reviewCommentWhereView(thread) + "\n"
		}

		return dimStyle.Render("  move onto a line to start a thread on it") + "\n"
	}

	head := titleOf(m.isReviewThreadFocused(), clipName(review.FormatLocation(anchor.FilePath, anchor.OldStart, anchor.NewStart)))
	if thread, ok := m.reviewThreadAtCursor(); ok {
		head += m.reviewCommentWhereView(thread)
	}

	return head + "\n"
}

// reviewCommentWhereView is which comment of how many the cursor is standing on.
func (m model) reviewCommentWhereView(thread *agentv1.ReviewThread) string {
	at, total := m.review.view.CommentPosition(thread)
	if at == 0 || total < 2 {
		return ""
	}

	return dimStyle.Render(fmt.Sprintf("  %d of %d", at, total))
}

// reviewReleaseView asks what to send: an optional word about the work as a whole, and the
// verdict the agent acts on.
func (m model) reviewReleaseView() string {
	out := "\n" + dimStyle.Render("  anything to say about the work as a whole? (optional)") + "\n"
	out += m.review.editor.View() + "\n\n"

	for i, verdict := range reviewVerdicts {
		mark, style := "  ", dimStyle
		if i == m.review.verdict {
			mark, style = "▸ ", pickVerdictStyle(verdict)
		}

		out += style.Render("  "+mark+describeVerdict(verdict)) + "\n"
	}

	return out + "\n" + dimStyle.Render("  tab to change · "+
		keyLabel(formKeys().Submit)+" send · "+
		keyLabel(formKeys().SendNow)+" send and interrupt · "+
		keyLabel(formKeys().Editor)+" editor · "+keyLabel(formKeys().Back)+" cancel") + "\n"
}

// describeVerdict is how a verdict reads in the panel, spelled the way a person would say it.
func describeVerdict(verdict agentv1.ReviewVerdict) string {
	switch verdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		return "approve"
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return "request changes"
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED, agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
		return "comment only"
	}

	return "no verdict"
}

// pickVerdictStyle colours the chosen verdict by what it means.
func pickVerdictStyle(verdict agentv1.ReviewVerdict) lipgloss.Style {
	switch verdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		return reviewDeliveredStyle
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return reviewSentStyle
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED, agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
	}

	return reviewUnsentStyle
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

// reviewOriginView is where a thread was written and the line it was written against, drawn only
// once that code has moved.
func (m model) reviewOriginView(thread *agentv1.ReviewThread) string {
	if !m.review.view.HasMoved(thread) {
		return ""
	}

	out := dimStyle.Render("  "+m.reviewOriginSaid(thread)) + "\n"
	for _, quoted := range thread.GetQuotedLines() {
		out += reviewQuoteStyle.Render(m.reviewWrap("  "+quoted)) + "\n"
	}

	return out + "\n"
}

// reviewOriginSaid says why a comment is not on the line it was written against, in terms of what
// happened rather than of the placement weave settled on.
func (m model) reviewOriginSaid(thread *agentv1.ReviewThread) string {
	if _, drawn := m.review.view.GetFile(thread.GetFilePath()); !drawn {
		if m.review.base != "" {
			return "still in the change, outside what is on screen · written against"
		}

		return "the file this was about has gone · written against"
	}

	if m.review.view.IsGhosted(thread) {
		return "the code under this has changed · written against"
	}

	return "moved since it was written · written against"
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

	return head + "\n" + m.reviewWrap(message.GetBody()) + "\n" + reviewDeliveryBadge(message) + "\n"
}

// reviewSendDesc says what the send key opens, since a verdict with nothing written is an
// approval rather than a delivery.
func (m model) reviewSendDesc() string {
	if m.doesReviewHoldUnsent() {
		return "send to the agent"
	}

	return "give a verdict"
}

// reviewDetails is the record drawn under the review sidebar, which is the agent whose diff is
// up together with what the diff itself was measured against.
func (m model) reviewDetails() details {
	index := slices.IndexFunc(m.listing.agents, func(item agentItem) bool { return item.agent.ID == m.review.agentID })
	if index < 0 {
		return details{}
	}

	item := m.listing.agents[index]
	agent := item.agent
	fields := agentFields(agent, m.scheduleName(agent), m.parentName(agent))

	return details{
		id:      agent.ID,
		notices: agentNotices(item),
		fields:  append(fields, m.reviewFields()...),
	}
}

// reviewFields are the facts the diff itself carries, which the agent's own record does not
// hold: what it was measured against and what was kept out of it.
func (m model) reviewFields() []recordField {
	fields := []recordField{
		{"base", orDash(shortSHA(m.review.round.GetBaseSha()))},
		{"snapshot", orDash(shortSHA(m.review.round.GetSnapshotSha()))},
	}
	if len(m.review.exclude) > 0 {
		fields = append(fields, recordField{"excludes", strings.Join(m.review.exclude, ", ")})
	}

	return fields
}

// reviewMarkerStyle is what the counts above and below the conversation are drawn in, which is
// the panel's own colour while the keys are on the thread rather than on the diff.
func (m model) reviewMarkerStyle() lipgloss.Style {
	if m.isReviewThreadFocused() {
		return headerStyle
	}

	return dimStyle
}

func (m model) reviewWrap(body string) string {
	return lipgloss.NewStyle().Width(max(20, m.paneWidth()-2)).Render(body)
}

func reviewTimeOf(stamp string) string {
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return ""
	}

	return at.Local().Format("3:04PM")
}

// reviewComposeDesc says what the compose key would do where the cursor is standing.
func (m model) reviewComposeDesc() string {
	if _, ok := m.reviewThreadAtCursor(); ok {
		return "reply in this thread"
	}

	if m.review.activity.delivery[deliverySlot(agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT)] == 0 {
		return "start a review here"
	}

	return "comment on this line"
}

// reviewBackDesc says which of the two things the back key leaves, since writing is a mode inside
// the screen and it is given up first.
func (m model) reviewBackDesc() string {
	if m.review.writing {
		return "stop writing"
	}

	return "back to the agents"
}

// reviewReloadDesc says what the reload key would do.
func (m model) reviewReloadDesc() string {
	if m.review.moved {
		return "catch up"
	}

	return "read the diff again"
}

// shortSHA is how a commit is named in the record, where the whole of it would take the column
// and say no more than its first few characters do.
func shortSHA(sha string) string {
	if len(sha) <= 12 {
		return sha
	}

	return sha[:12]
}

// reviewResolveDesc says which way the settle key would go where the cursor is standing.
func (m model) reviewResolveDesc() string {
	if thread, ok := m.reviewThreadAtCursor(); ok && thread.GetResolvedAt() != "" {
		return "reopen the thread"
	}

	return "settle the thread"
}

// reviewViewedDesc says which way the fold key would go where the cursor is standing.
func (m model) reviewViewedDesc() string {
	if m.review.view == nil {
		return "mark the file viewed"
	}

	if file, ok := m.review.view.GetFileAt(m.review.cursor); ok && m.review.view.IsViewed(file.Path()) {
		return "open the file again"
	}

	return "mark the file viewed"
}
