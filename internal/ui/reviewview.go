package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/internal/review"
	"weave/internal/ui/paint"
	"weave/internal/ui/paint/label"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
)

// The tones the review wears, which the region drawing each one turns into a colour.
const (
	reviewQuoteTone  = paint.ToneMuted
	reviewHumanTone  = paint.ToneSelf
	reviewAgentTone  = paint.ToneOther
	reviewNoticeTone = paint.ToneSuccess
	reviewAddedTone  = paint.ToneSuccess
	// reviewMovedTone draws the catch-up notice as coloured text rather than as a filled bar, the
	// agent writing while you read being ordinary rather than an alarm.
	reviewMovedTone     = paint.ToneWarning
	reviewRemovedTone   = paint.ToneDanger
	reviewUnsentTone    = paint.ToneAccent
	reviewSentTone      = paint.ToneWarning
	reviewDeliveredTone = paint.ToneSuccess
)

// reviewDeadStyle says there is nobody left to send a review to.
func reviewDeadStyle(palette paint.Palette) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(palette.Color(paint.ToneMuted)).Bold(true)
}

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
	record := m.detailsView()
	messages := m.messagesRegion(record, help, above...)
	chrome := slices.Concat(above, []string{help, record, messages})
	head := m.reviewHeadView(reviewOwedRoom(m.listLines(chrome...)))
	rest := m.review.threads.window(m.reviewRestView(), m.listLines(slices.Concat(chrome, []string{head})...))

	return paint.PadRows(head+rest, m.listLines(chrome...)) + record + messages + help
}

// window cuts the conversation down to the rows the sidebar has, keeping its end unless the
// reader has taken the keys and scrolled up into it.
func (t reviewThreads) window(rendered string, lines int) string {
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
		return len(rows) - visible - min(t.scroll, len(rows)-visible)
	}

	return paint.WindowRows(rows, lines, place, reviewMarkerStyle())
}

// doesReviewFitDetails reports whether the sidebar has a line left over once the record has taken
// its share.
func (m model) doesReviewFitDetails() bool {
	// Measured with the keys on the diff, which keeps this out of its own answer.
	m.details.focused, m.details.filter = false, filterState{}

	spare := m.spareLines(m.renderHelp(m.reviewHelp()), m.reviewHeadView(owedRows))

	return spare > m.detailsLines()+detailsChromeLines
}

// reviewDetailsRoom is the rows the sidebar has under the head and the legend it is drawing.
func (m model) reviewDetailsRoom() int {
	return m.spareLines(m.renderHelp(m.reviewHelp()), m.reviewHeadView(owedRows))
}

// reviewHeadView is the fixed top of the sidebar: whatever is standing in the way of a review, or
// the place the cursor is standing when nothing is.
func (m model) reviewHeadView(owedRoom int) string {
	switch {
	case m.review.loading:
		return dimStyle.Render(reviewLoadingWord) + "\n"
	case m.review.diff.view == nil || m.review.diff.view.Len() == 0:
		return dimStyle.Render(reviewEmptyWord) + "\n"
	}

	return m.reviewOwedView(owedRoom) + m.review.diff.anchorView()
}

// reviewNote is a line of prose under the conversation, wrapped into the sidebar's own column.
func (m model) reviewNote(style lipgloss.Style, text string) string {
	var out strings.Builder
	for _, row := range paint.WrapRows(text, m.lineWidth()-2) {
		out.WriteString(style.Render("  "+row) + "\n")
	}

	return out.String()
}

func (m model) reviewRestView() string {
	if m.review.loading || m.review.diff.view == nil || m.review.diff.view.Len() == 0 {
		return ""
	}

	if m.review.threads.composing {
		return "\n" + m.review.threads.editor.view() + "\n"
	}

	if m.review.threads.releasing {
		return m.reviewReleaseView()
	}

	out := "\n" + m.reviewThreadView()

	// A notice answers the last keypress and a keypress clears it, so what the reader is told
	// about the diff itself stands underneath and comes back the moment the answer is gone.
	if notice := m.review.notice; notice != "" {
		out += "\n" + m.reviewNote(paint.Panel.Style(reviewNoticeTone), notice)
	} else if since := m.review.diff.since; since != "" && m.review.diff.baseLabel == "" {
		out += "\n" + m.reviewNote(dimStyle, since)
	}

	return out
}

// anchorView names the place the cursor is standing, so the sidebar says what a new thread
// would be pinned to without repeating the line the diff already draws beside it.
func (d reviewDiff) anchorView() string {
	anchor, ok := d.view.GetAnchor(d.cursor)
	if !ok {
		if thread, ok := d.threadAtCursor(); ok {
			return paint.Panel.Title(clipName(review.FormatThreadLocation(thread))) +
				d.commentWhereView(thread) + "\n"
		}

		return dimStyle.Render("  move onto a line to start a thread on it") + "\n"
	}

	head := paint.Panel.Title(clipName(review.FormatLocation(anchor.FilePath, anchor.OldStart, anchor.NewStart)))
	if thread, ok := d.threadAtCursor(); ok {
		head += d.commentWhereView(thread)
	}

	return head + "\n"
}

// commentWhereView is which comment of how many the cursor is standing on.
func (d reviewDiff) commentWhereView(thread *agentv1.ReviewThread) string {
	at, total := d.view.CommentPosition(thread)
	if at == 0 || total < 2 {
		return ""
	}

	return dimStyle.Render(fmt.Sprintf("  %d of %d", at, total))
}

// reviewReleaseView asks what to send: an optional word about the work as a whole, and the
// verdict the agent acts on.
func (m model) reviewReleaseView() string {
	out := "\n" + m.review.threads.editor.view() + "\n\n"

	for i, verdict := range reviewVerdicts {
		mark, style := "  ", dimStyle
		if i == m.review.threads.verdict {
			mark, style = "▸ ", paint.Panel.Style(verdictTone(verdict))
		}

		out += style.Render("  "+mark+describeVerdict(verdict)) + "\n"
	}

	return out + "\n" + m.reviewNote(dimStyle, "tab to change · "+
		keyLabel(formKeys().SendNow)+" send and interrupt · "+
		keyLabel(formKeys().Back)+" cancel")
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

// verdictTone colours the chosen verdict by what it means.
func verdictTone(verdict agentv1.ReviewVerdict) paint.Tone {
	switch verdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		return reviewDeliveredTone
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return reviewSentTone
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED, agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
	}

	return reviewUnsentTone
}

func (m model) reviewThreadView() string {
	thread, ok := m.review.diff.threadAtCursor()
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
	if !m.review.diff.view.HasMoved(thread) {
		return ""
	}

	out := dimStyle.Render("  "+m.review.diff.originSaid(thread)) + "\n"
	for _, quoted := range thread.GetQuotedLines() {
		out += paint.Panel.Style(reviewQuoteTone).Italic(true).Render(m.reviewWrap("  "+quoted)) + "\n"
	}

	return out + "\n"
}

// originSaid says why a comment is not on the line it was written against, in terms of what
// happened rather than of the placement weave settled on.
func (d reviewDiff) originSaid(thread *agentv1.ReviewThread) string {
	if _, drawn := d.view.GetFile(thread.GetFilePath()); !drawn {
		if d.base != "" {
			return "still in the change, outside what is on screen · written against"
		}

		return "the file this was about has gone · written against"
	}

	if d.view.IsGhosted(thread) {
		return "the code under this has changed · written against"
	}

	return "moved since it was written · written against"
}

func (m model) reviewMessageView(message *agentv1.ReviewMessage) string {
	author, tone := "you", reviewHumanTone
	if message.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
		author, tone = m.review.agentName, reviewAgentTone
	}

	head := paint.Panel.Style(tone).Bold(true).Render(author)
	if when := reviewTimeOf(message.GetCreatedAt()); when != "" {
		head += " " + dimStyle.Render(when)
	}

	return head + "\n" + m.reviewWrap(message.GetBody()) + "\n" + reviewDeliveryBadge(paint.Panel, message) + "\n"
}

// sendDesc says what the send key opens.
func (t reviewThreads) sendDesc() string {
	if t.holdsUnsent() {
		return "send to the agent"
	}

	return "give a verdict"
}

// reviewDetails is the record drawn under the review sidebar.
func (m model) reviewDetails() details {
	index := slices.IndexFunc(m.listing.agents, func(item agentItem) bool { return item.agent.ID == m.review.agentID })
	if index < 0 {
		return details{}
	}

	item := m.listing.agents[index]
	agent := item.agent
	fields := m.agentDetailFields(item)

	// The notices read the row the list would draw, and which columns that row fills depends on
	// whether the agent owns its checkout, which only its workflow says.
	row := agentRow{item: item}
	if workflow := m.workflowByID(agent.WorkflowID); workflow != nil {
		row.workflow = *workflow
	}

	return details{
		id:      agent.ID,
		notices: agentNotices(row),
		fields:  append(fields, m.review.fields()...),
	}
}

// fields are the facts the diff itself carries, which the agent's own record does not hold: what
// it was measured against and what was kept out of it.
func (r reviewState) fields() []recordField {
	fields := []recordField{
		{"base", label.OrDash(label.ShortSHA(r.round.GetBaseSha()))},
		{"snapshot", label.OrDash(label.ShortSHA(r.round.GetSnapshotSha()))},
	}
	if len(r.exclude) > 0 {
		fields = append(fields, recordField{"excludes", strings.Join(r.exclude, ", ")})
	}

	return fields
}

// reviewMarkerStyle is what the counts above and below the conversation are drawn in.
func reviewMarkerStyle() lipgloss.Style {
	return paint.Panel.Header()
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

// composeDesc says what the compose key would do where the cursor is standing.
func (r reviewState) composeDesc() string {
	if _, ok := r.diff.threadAtCursor(); ok {
		return "reply in this thread"
	}

	if r.threads.isOwedAnAnswer() {
		return "reply in a comment left open"
	}

	if !r.threads.holdsUnsent() {
		return "start a review here"
	}

	return "comment on this line"
}

// reviewBackDesc says which of the two things the back key leaves.
func (m model) reviewBackDesc() string {
	if m.review.mode == modeReviewing {
		return "stop reviewing"
	}

	return "back to the agents"
}

// reloadDesc says what the reload key would do.
func (d reviewDiff) reloadDesc() string {
	if d.moved {
		return "catch up"
	}

	return "read the diff again"
}

// resolveDesc says which way the settle key would go where the cursor is standing.
func (d reviewDiff) resolveDesc() string {
	if thread, ok := d.threadAtCursor(); ok && thread.GetResolvedAt() != "" {
		return "reopen the thread"
	}

	return "settle the thread"
}

// fileDesc names the two keys that act on the whole file the cursor is in, which share a legend
// row so that the taller of them costs the pane nothing.
func (d reviewDiff) fileDesc() string {
	if d.view == nil {
		return "fold / whole file"
	}

	// A folded file has nothing on screen to open out, so the expand key is refused there and
	// the legend names the fold key alone.
	file, ok := d.view.GetDrawnFileAt(d.cursor)

	// Marking a file read is refused in a round, so the row names the key that still works.
	if d.isNarrowed() {
		if ok && d.view.IsExpanded(file.Path()) {
			return "show the changes"
		}

		return "show the whole file"
	}

	if ok && d.view.IsViewed(file.Path()) {
		return "open the file"
	}

	if ok && d.view.IsExpanded(file.Path()) {
		return "fold / the changes"
	}

	return "fold / whole file"
}
