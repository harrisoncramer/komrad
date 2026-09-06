package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/internal/review"
	agentv1 "weave/proto/gen/agent/v1"

	"github.com/charmbracelet/bubbles/key"
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

// reviewDeliveryWord is what each state is called wherever it is drawn, so the count at the top
// of the sidebar and the badge under a comment read as the same thing.
func reviewDeliveryWord(state agentv1.ReviewDeliveryState) string {
	switch state {
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT:
		return "unsent"
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT:
		return "waiting"
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED:
		return "delivered"
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED:
		return ""
	}

	return ""
}

func reviewDeliveryStyle(state agentv1.ReviewDeliveryState) lipgloss.Style {
	switch state {
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT:
		return reviewUnsentStyle
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT:
		return reviewSentStyle
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED:
		return reviewDeliveredStyle
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED:
		return dimStyle
	}

	return dimStyle
}

// reviewDeliveryMark is the glyph a badge opens with, the same one the gutter uses where the
// gutter has one, so a mark beside a line and a badge under a comment are read together.
func reviewDeliveryMark(state agentv1.ReviewDeliveryState) string {
	switch state {
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSENT:
		return "◇"
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_SENT:
		return "→"
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_DELIVERED:
		return "✓"
	case agentv1.ReviewDeliveryState_REVIEW_DELIVERY_STATE_UNSPECIFIED:
		return ""
	}

	return ""
}

// reviewView draws the sidebar while the review is up, which is the thread the cursor is
// standing on and the line it hangs off, since the diff itself is in the stage.
func (m model) reviewView() string {
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

// reviewFitsDetails is whether the sidebar has a line left over once the record has taken its
// share, since this one carries a head and a conversation the lists do not and drawing the
// record anyway would run past the bottom of the pane.
func (m model) reviewFitsDetails() bool {
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

// reviewStaleView is the bar saying the agent has written since this round was taken, drawn above
// the place the cursor is standing, since catching up is always the reader's to ask for.
func (m model) reviewStaleView() string {
	if !m.review.moved {
		return ""
	}

	line := " THE AGENT HAS MOVED ON · " + keyLabel(m.keys.Review.Reload) + " to catch up "

	return reviewStaleStyle.Render(m.reviewFill(line)) + "\n"
}

// reviewFill pads a line out to the sidebar's width, so a background colour reads as a bar
// rather than as a highlighted phrase.
func (m model) reviewFill(line string) string {
	width := max(20, m.width-2)
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
		return "\n" + m.review.editor.View() + "\n" +
			dimStyle.Render("  "+keyLabel(m.keys.Form.Submit)+" save · esc cancel") + "\n"
	}

	out := "\n" + m.reviewThreadView()

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
			return m.nameStyle(true, false, true).Render(clipName(review.FormatThreadLocation(thread))) + "\n"
		}

		return dimStyle.Render("  move onto a line to start a thread on it") + "\n"
	}

	return m.nameStyle(true, false, true).Render(clipName(review.FormatLocation(anchor.FilePath, anchor.OldStart, anchor.NewStart))) + "\n"
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

	files := fmt.Sprintf("%d file", stat.Files)
	if stat.Files != 1 {
		files += "s"
	}

	out := dimStyle.Render(files+" changed") + "  " +
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

	where := matchesFound(m.review.view.MatchCount())
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

func matchesFound(count int) string {
	if count == 1 {
		return "1 match"
	}

	return fmt.Sprintf("%d matches", count)
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
	state := message.GetDelivery()

	word := reviewDeliveryWord(state)
	if word == "" {
		return ""
	}

	badge := reviewDeliveryMark(state) + " " + word
	if when := reviewTimeOf(reviewDeliveryStamp(message)); when != "" {
		badge += " " + when
	}

	return reviewDeliveryStyle(state).Render("  "+badge) + "\n"
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
	counted := m.reviewDeliveryCounts()

	parts := make([]string, 0, len(reviewDeliveryOrder))
	for _, state := range reviewDeliveryOrder {
		if counted[state] == 0 {
			continue
		}

		parts = append(parts, reviewDeliveryStyle(state).Render(
			fmt.Sprintf("%d %s", counted[state], reviewDeliveryWord(state))))
	}

	if len(parts) == 0 {
		return dimStyle.Render("nothing written yet")
	}

	return strings.Join(parts, dimStyle.Render(" · "))
}

// reviewDeliveryCounts is how many of the person's comments stand in each state, counted over
// every thread since delivery is a property of the comment rather than of where it is pinned.
func (m model) reviewDeliveryCounts() map[agentv1.ReviewDeliveryState]int {
	counted := map[agentv1.ReviewDeliveryState]int{}
	for _, thread := range m.review.threads {
		for _, message := range thread.GetMessages() {
			counted[message.GetDelivery()]++
		}
	}

	return counted
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

func (m model) reviewHelp() keyMap {
	keys := m.keys.Review

	move := m.moveBinding()

	// An agent nobody can write to keeps every key that reads and loses every key that sends, so
	// the ones that would be refused cannot be pressed by accident.
	var writing, composing []key.Binding
	if _, dead := m.reviewIsReadOnly(); !dead {
		composing = []key.Binding{binding(keys.Compose, m.reviewComposeDesc())}
		writing = slices.Concat(composing, []key.Binding{
			binding(keys.Approve, "approve the work"),
			binding(m.keys.Form.Submit, "request changes"),
		})
	}

	// Only the things the screen is for are on the strip, since reviewing is reading and the
	// rest of the keys are behind the help key.
	return keyMap{
		short: slices.Concat([]key.Binding{move}, composing, []key.Binding{m.helpBinding()}),
		full: slices.Concat(
			[]key.Binding{
				move,
				pairBinding(keys.PageDown, keys.PageUp, "page"),
				pairBinding(keys.HalfPageDown, keys.HalfPageUp, "half a page"),
				pairBinding(keys.NextFile, keys.PrevFile, "next/previous file"),
				pairBinding(keys.NextHunk, keys.PrevHunk, "next/previous hunk"),
				pairBinding(keys.NextThread, keys.PrevThread, "next/previous thread"),
				binding(m.keys.Filter.Open, "search"),
				pairBinding(keys.NextMatch, keys.PrevMatch, "next/previous match"),
				binding([]string{recentrePrefix + "z", recentrePrefix + "t", recentrePrefix + "b"}, "recentre the view"),
				pairBinding(keys.PanLeft, keys.PanRight, "pan sideways"),
				binding(keys.EditFile, m.reviewEditorDesc()),
				binding(keys.Reload, m.reviewReloadDesc()),
				binding(keys.Resolve, m.reviewResolveDesc()),
				binding(keys.ToggleViewed, m.reviewViewedDesc()),
				m.detailsBinding(),
			},
			writing,
			[]key.Binding{binding(keys.Back, "back to the agents")},
			m.globalHelp(),
		),
	}
}
