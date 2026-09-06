package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/internal/review"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/lipgloss"
)

var (
	reviewQuoteStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	reviewHumanStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	reviewAgentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	reviewNoticeStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	reviewAddedStyle   = lipgloss.NewStyle().Foreground(colorSuccess)
	reviewRemovedStyle = lipgloss.NewStyle().Foreground(colorDanger)

	// Reversed rather than merely coloured: a reader who keeps writing against a patch that has
	// already moved has those comments pinned as ghosted or adrift when they finally reload, so
	// this is worth a bar across the sidebar rather than a dim line they read past.
	reviewStaleStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(colorWarning).Bold(true)
	reviewDeadStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(colorMuted).Bold(true)
	reviewUnsentStyle    = lipgloss.NewStyle().Foreground(colorAccent)
	reviewSentStyle      = lipgloss.NewStyle().Foreground(colorWarning)
	reviewDeliveredStyle = lipgloss.NewStyle().Foreground(colorSuccess)
)

// reviewDeliveryOrder is the three states a comment can stand in, ordered by how much they are
// owed: what never went, what is waiting, what landed.
var reviewDeliveryOrder = []agentv1.ReviewDeliveryState{
	agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT,
	agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT,
	agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED,
}

// reviewDelivery is how one delivery state is drawn: what it is called, the glyph a badge opens
// with, and the colour both of those carry.
type reviewDelivery struct {
	word  string
	mark  string
	style lipgloss.Style
}

// reviewDeliveryOf is how a state is drawn wherever it appears, so the count at the top of the
// sidebar, the badge under a comment and the gutter's own mark read as one thing.
func reviewDeliveryOf(state agentv1.ReviewDeliveryState) reviewDelivery {
	switch state {
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT:
		return reviewDelivery{
			word:  "unsent",
			mark:  "◇",
			style: reviewUnsentStyle,
		}
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT:
		return reviewDelivery{
			word:  "waiting",
			mark:  "→",
			style: reviewSentStyle,
		}
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED:
		return reviewDelivery{
			word:  "delivered",
			mark:  "✓",
			style: reviewDeliveredStyle,
		}
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED:
		return reviewDelivery{style: dimStyle}
	}

	return reviewDelivery{style: dimStyle}
}

// reviewView draws the diff and, beside it, the thread the cursor is standing on, as one frame
// so that the cursor and the conversation under it can never disagree about the line being read.
func (m model) reviewView() string {
	// Trimmed so the two halves are the same height, since a trailing newline is a row the
	// sidebar block would have and the diff beside it would not.
	sidebar := strings.TrimRight(m.reviewSidebarView(), "\n")

	split, ok := m.reviewSplit()
	if !ok {
		return sidebar
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, m.reviewDiffView(split), sidebar)
}

// reviewSidebarView is the panel's own column: where the cursor is standing and the thread
// hanging off it.
func (m model) reviewSidebarView() string {
	header := m.reviewHeader()
	help := m.renderHelp(m.reviewHelp())

	// The head is pinned and the conversation gives up the room: where the cursor is standing is
	// what makes the rest of the pane mean anything.
	head := m.reviewHeadView()
	record := m.detailsView()
	rest := tailRows(m.reviewRestView(), m.listLines(header, help, record, head))

	return header + padRows(head+rest, m.listLines(header, help, record)) + record + help
}

func (m model) reviewHeader() string {
	return m.titleStyle().Render("Review · "+m.review.agentName) + "\n" +
		m.reviewStatView() +
		m.reviewDeliveryView() + "\n\n" +
		m.review.filter.view() + m.reviewMatchCount()
}

// doesReviewFitDetails reports whether the sidebar has a line left over once the record has taken its
// share, since this one carries a head and a conversation the lists do not and drawing the
// record anyway would run past the bottom of the pane.
func (m model) doesReviewFitDetails() bool {
	// Measured with the keys on the diff, which keeps this out of its own answer, since focus is
	// only ever taken by a key handler and so a copy that starts unfocused stays that way.
	m.details.focused = false

	spare := m.spareLines(m.reviewHeader(), m.renderHelp(m.reviewHelp()), m.reviewHeadView())

	return spare > m.detailsLines()+detailsChromeLines
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

	return m.reviewDeadView() + m.reviewStaleView() + m.reviewAnchorView()
}

// reviewDeadView says there is nobody on the other end, since the diff opens for an agent that
// cannot be written to and every key that would write is refused there.
func (m model) reviewDeadView() string {
	reason, dead := m.reviewIsReadOnly()
	if !dead {
		return ""
	}

	return reviewDeadStyle.Render(m.reviewFill(" "+reason+" ")) + "\n"
}

// reviewStaleView is the bar saying the worktree has been written to since this round was taken,
// naming the worktree rather than the agent because a snapshot records what a tree holds and never
// who put it there.
func (m model) reviewStaleView() string {
	if !m.review.moved {
		return ""
	}

	line := " THE WORKTREE HAS MOVED ON · " + keyLabel(m.keys.Review.Reload) + " to catch up "

	return reviewStaleStyle.Render(m.reviewFill(line)) + "\n"
}

// reviewFill pads a line out to the sidebar's width, so a background colour reads as a bar
// rather than as a highlighted phrase.
func (m model) reviewFill(line string) string {
	width := max(20, m.paneWidth()-2)
	if run := lipgloss.Width(line); run < width {
		line += strings.Repeat(" ", width-run)
	}

	return line
}

func (m model) reviewRestView() string {
	if m.review.err != nil || m.review.loading || m.review.view == nil || m.review.view.Len() == 0 {
		return ""
	}

	if m.review.composing {
		out, leaving := "\n", "esc cancel"
		if held, ok := m.heldSuggestion(); ok {
			out += reviewNoticeStyle.Render("  "+m.describeSuggestion(held)) + "\n"
			leaving = "esc turn it down"
		}

		return out + m.review.editor.View() + "\n" +
			dimStyle.Render("  "+keyLabel(m.keys.Form.Submit)+" save · "+leaving) + "\n"
	}

	if m.review.releasing {
		return m.reviewReleaseView()
	}

	out := "\n" + m.reviewThreadView()

	if held, ok := m.heldSuggestion(); ok {
		out += "\n" + reviewNoticeStyle.Render("  "+m.describeSuggestion(held)) + "\n" +
			dimStyle.Render("  "+keyLabel(m.keys.Review.Compose)+" to comment with it") + "\n"
	}

	if adrift := m.review.adrift; len(adrift) > 0 {
		out += "\n" + dimStyle.Render("  "+describeAdriftEdits(adrift)) + "\n"
	}

	// A notice answers the last keypress and a keypress clears it, so what the reader is told
	// about the diff itself stands underneath and comes back the moment the answer is gone.
	if notice := m.review.notice; notice != "" {
		out += "\n" + reviewNoticeStyle.Render("  "+notice) + "\n"
	} else if since := m.review.since; since != "" {
		out += "\n" + dimStyle.Render("  "+since) + "\n"
	}

	return out
}

// reviewAnchorView names the place the cursor is standing, so the sidebar says what a new thread
// would be pinned to without repeating the line the diff is already drawing beside it.
func (m model) reviewAnchorView() string {
	anchor, ok := m.review.view.GetAnchor(m.review.cursor)
	if !ok {
		if thread, ok := m.reviewThreadAtCursor(); ok {
			return m.nameStyle(true, true).Render(clipName(review.FormatThreadLocation(thread))) + "\n"
		}

		return dimStyle.Render("  move onto a line to start a thread on it") + "\n"
	}

	return m.nameStyle(true, true).Render(clipName(review.FormatLocation(anchor.FilePath, anchor.OldStart, anchor.NewStart))) + "\n"
}

// heldSuggestion is the edit the next comment would carry, which is the front of the queue since
// the reviewer answers for one at a time.
func (m model) heldSuggestion() (pendingSuggestion, bool) {
	if len(m.review.suggestions) == 0 {
		return pendingSuggestion{}, false
	}

	return m.review.suggestions[0], true
}

// describeAdriftEdits names what the reviewer wrote on lines the diff does not draw, since they
// wrote it on purpose and the text is still sitting in the checkout for them to go back to.
func describeAdriftEdits(adrift []review.Suggestion) string {
	where := review.FormatLocation(adrift[0].FilePath, 0, adrift[0].NewStart)
	if len(adrift) > 1 {
		where = fmt.Sprintf("%s and %d more", where, len(adrift)-1)
	}

	return where + " is not in the diff, so that edit stays in the round's checkout"
}

// describeSuggestion names the edit of the round waiting to be sent, since it is pinned where it
// was made rather than under the cursor and the reader would otherwise have to guess where.
func (m model) describeSuggestion(held pendingSuggestion) string {
	line := fmt.Sprintf("edit held for %s (%s)",
		review.FormatLocation(held.anchor.FilePath, held.anchor.OldStart, held.anchor.NewStart),
		counted(len(held.lines), "line"))

	if waiting := len(m.review.suggestions) - 1; waiting > 0 {
		line += fmt.Sprintf(", %d more behind it", waiting)
	}

	return line
}

// reviewReleaseView asks what to send: a word about the work as a whole, which is optional, and
// the verdict, which is the part worth choosing deliberately since it is what the agent acts on.
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
		keyLabel(m.keys.Form.Submit)+" send · esc cancel") + "\n"
}

// describeVerdict is how a verdict reads in the panel, spelled the way a person would say it
// rather than the way the wire does.
func describeVerdict(verdict agentv1.ReviewVerdict) string {
	switch verdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		return "approve"
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return "request changes"
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED, agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
		return "comment only"
	default:
		return "no verdict"
	}
}

// pickVerdictStyle colours the chosen verdict by what it means, since approving and asking for
// more work are the two outcomes worth telling apart at a glance.
func pickVerdictStyle(verdict agentv1.ReviewVerdict) lipgloss.Style {
	switch verdict {
	case agentv1.ReviewVerdict_REVIEW_VERDICT_APPROVED:
		return reviewDeliveredStyle
	case agentv1.ReviewVerdict_REVIEW_VERDICT_CHANGES_REQUESTED:
		return reviewSentStyle
	case agentv1.ReviewVerdict_REVIEW_VERDICT_COMMENTED, agentv1.ReviewVerdict_REVIEW_VERDICT_UNSPECIFIED:
		return reviewUnsentStyle
	default:
		return reviewUnsentStyle
	}
}

// reviewStatView is how big the change is, with the counts coloured the way the lines
// themselves are so which number is which can be read at a glance.
func (m model) reviewStatView() string {
	if m.review.view == nil {
		return ""
	}

	stat := m.review.view.Stat()
	if stat.Files == 0 {
		return ""
	}

	out := dimStyle.Render(counted(stat.Files, "file")+" changed") + "  " +
		reviewAddedStyle.Render(fmt.Sprintf("+%d", stat.Added)) + " " +
		reviewRemovedStyle.Render(fmt.Sprintf("-%d", stat.Removed))

	if read, total := m.review.view.CountViewedFiles(); read > 0 {
		out += dimStyle.Render(fmt.Sprintf("  %d/%d viewed", read, total))
	}

	return out + "\n"
}

// reviewMatchCount is where in the search the cursor is standing, drawn under the pattern the
// panel's own filter is already showing.
func (m model) reviewMatchCount() string {
	if m.review.view == nil || !m.review.filter.isStanding() {
		return ""
	}

	where := counted(m.review.view.MatchCount(), "match")
	if at, ok := m.review.view.MatchNumber(m.review.cursor); ok {
		where = fmt.Sprintf("%d of %d", at, m.review.view.MatchCount())
	}

	// A match inside a file folded away is on no row, so stepping through the matches never
	// reaches it, and it is counted apart rather than left to look like nothing was found.
	if hidden := m.review.view.HiddenMatches(); hidden > 0 {
		where += fmt.Sprintf(" · %d in viewed files", hidden)
	}

	return dimStyle.Render("  "+where) + "\n\n"
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

// reviewOriginView is where a thread was written and the line it was written against, drawn
// only once that code has moved, since the diff alongside already shows a thread that has not.
func (m model) reviewOriginView(thread *agentv1.ReviewThread) string {
	if !m.review.view.HasMoved(thread) {
		return ""
	}

	out := dimStyle.Render("  written against") + "\n"
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

	return head + "\n" + m.reviewWrap(message.GetBody()) + "\n" + reviewDeliveryBadge(message) + "\n"
}

// reviewDeliveryBadge is how far one comment has got towards being read, drawn under the comment
// itself because how the whole review is doing does not answer what this one is doing, and drawn
// not at all for what the agent wrote, since nobody is waiting to be told about that.
func reviewDeliveryBadge(message *agentv1.ReviewMessage) string {
	drawn := reviewDeliveryOf(message.GetDelivery())
	if drawn.word == "" {
		return ""
	}

	badge := drawn.mark + " " + drawn.word
	if when := reviewTimeOf(reviewDeliveryStamp(message)); when != "" {
		badge += " " + when
	}

	return drawn.style.Render("  "+badge) + "\n"
}

// reviewDeliveryStamp is the moment a badge names, which is the last thing that happened to the
// comment rather than when it was written.
func reviewDeliveryStamp(message *agentv1.ReviewMessage) string {
	if delivered := message.GetDeliveredAt(); delivered != "" {
		return delivered
	}

	return message.GetSentAt()
}

// reviewDeliveryView is how the person's own comments stand, counted rather than summarised: one
// phrase for the whole review could never say what any one comment was doing, which is the thing
// per-comment delivery exists to answer.
func (m model) reviewDeliveryView() string {
	parts := make([]string, 0, len(reviewDeliveryOrder))
	for _, state := range reviewDeliveryOrder {
		count := m.review.activity.delivery[deliverySlot(state)]
		if count == 0 {
			continue
		}

		drawn := reviewDeliveryOf(state)
		parts = append(parts, drawn.style.Render(fmt.Sprintf("%d %s", count, drawn.word)))
	}

	if len(parts) == 0 {
		return dimStyle.Render("nothing written yet")
	}

	return strings.Join(parts, dimStyle.Render(" · "))
}

// reviewIsReadOnly is whether there is nobody there to receive a comment, and what to say about
// it, since an archived agent has to be restored and a dormant one resumed before the review is
// a conversation rather than a read.
func (m model) reviewIsReadOnly() (string, bool) {
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

// reviewDetails is the record drawn under the review sidebar, which is the agent whose diff is
// up together with what the diff itself was measured against.
func (m model) reviewDetails() details {
	index := slices.IndexFunc(m.listing.agents, func(item agentItem) bool { return item.agent.ID == m.review.agentID })
	if index < 0 {
		return details{}
	}

	agent := m.listing.agents[index].agent
	fields := agentFields(agent, m.scheduleName(agent), m.parentName(agent))

	return details{
		id:     agent.ID,
		kind:   "Agent",
		title:  agent.Name,
		fields: append(fields, m.reviewFields()...),
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

// reviewComposeDesc says what the compose key would do where the cursor is standing, since it
// starts a thread on a line that has none and answers the one that is already there.
func (m model) reviewComposeDesc() string {
	if _, ok := m.reviewThreadAtCursor(); ok {
		return "reply in this thread"
	}

	return "start a thread here"
}

// reviewReloadDesc says what the reload key would do, since stepping to a round that does not
// exist yet and rereading the one already on screen are different things.
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
