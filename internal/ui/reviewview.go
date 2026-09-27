package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"komrad/internal/git"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/internal/ui/panel"
	agentv1 "komrad/proto/gen/agent/v1"

	"charm.land/bubbles/v2/key"
	"charm.land/lipgloss/v2"
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

// view draws the diff and, beside it, the thread the cursor is standing on, as one frame.
func (r reviewScreen) view(s panelShared) string {
	split, ok := s.draw.screenColumnSplit()
	if !ok {
		band := r.bandView(s, s.pane)

		return band + strings.TrimRight(r.sidebarView(s, band), "\n")
	}

	// Trimmed so the two halves are the same height.
	sidebar := strings.TrimRight(r.sidebarView(s), "\n")

	// The band stands over the diff rather than over the whole window, and what it carries acts on
	// the patch.
	diff := r.bandView(s, split.column) + r.diffView(s, split)

	return s.draw.joinColumns(split, sidebar, diff)
}

// sidebarView is the panel's own column: where the cursor is standing, and the thread hanging
// off it.
func (r reviewScreen) sidebarView(s panelShared, above ...string) string {
	layout := r.layoutSidebar(s, above...)
	rest := r.threads.window(r.restView(s), layout.threadLines)

	return paint.PadRows(layout.head+rest, layout.spare) + layout.record + layout.messages + layout.help
}

// sidebarLayout is the sidebar measured once, so the drawing and the thread's scroll agree on
// the rows the conversation has.
type sidebarLayout struct {
	help        string
	record      string
	messages    string
	head        string
	spare       int
	threadLines int
}

// layoutSidebar pins the head and hands the conversation whatever the chrome leaves: where the
// cursor is standing is what makes the rest of the pane mean anything.
func (r reviewScreen) layoutSidebar(s panelShared, above ...string) sidebarLayout {
	help := s.draw.renderHelp(r.help(s))
	record := s.draw.detailsView()
	messages := s.draw.messagesRegion(record, help, above...)
	chrome := slices.Concat(above, []string{help, record, messages})
	spare := s.draw.listLines(chrome...)
	head := r.headView(s, countYoursRoom(spare))

	return sidebarLayout{
		help:        help,
		record:      record,
		messages:    messages,
		head:        head,
		spare:       spare,
		threadLines: s.draw.listLines(slices.Concat(chrome, []string{head})...),
	}
}

// layoutSidebarAsDrawn measures the sidebar with the band over it when the panel draws one there.
func (r reviewScreen) layoutSidebarAsDrawn(s panelShared) sidebarLayout {
	if _, ok := s.draw.screenColumnSplit(); !ok {
		return r.layoutSidebar(s, r.bandView(s, s.pane))
	}

	return r.layoutSidebar(s)
}

// help keeps the strip to what the screen is for, and hands the legend to the conversation
// while the reader has taken the keys onto it.
func (r reviewScreen) help(s panelShared) keyMap {
	if r.isThreadHolding() {
		return r.threadHelp(s)
	}

	own := r.actions().help(s)

	return keyMap{
		short: slices.Concat(own.short, s.draw.globalHelp()),
		full: slices.Concat(
			own.full,
			[]key.Binding{s.draw.cycleBinding()},
			s.draw.fullGlobalHelp(),
		),
	}
}

// isThreadHolding reports whether the reader has taken the keys onto the conversation.
func (r reviewScreen) isThreadHolding() bool {
	return !r.isWriting() && r.isThreadTaken()
}

// threadHelp is the legend while the conversation holds the keys.
func (r reviewScreen) threadHelp(s panelShared) keyMap {
	scroll := binding(slices.Concat(s.keys.Nav.Up, s.keys.Nav.Down), "scroll the thread")
	ends := binding(slices.Concat(s.keys.Nav.Top, s.keys.Nav.Bottom), "top/bottom")
	back := binding([]string{"esc"}, "back to the diff")
	reply := binding(s.keys.Review.Compose, "reply")
	settle := binding(s.keys.Review.Resolve, r.diff.resolveDesc())
	send := binding(formKeys().Submit, r.threads.sendDesc())

	return keyMap{
		short: slices.Concat([]key.Binding{scroll, back, reply, settle}, s.draw.globalHelp()),
		full:  slices.Concat([]key.Binding{scroll, ends, back, reply, settle, send}, s.draw.fullGlobalHelp()),
	}
}

// window cuts the conversation down to the rows the sidebar has, keeping its end unless the
// reader has taken the keys and scrolled up into it.
func (t reviewThreads) window(rendered string, lines int) string {
	if lines <= 0 {
		return ""
	}

	if len(strings.Split(strings.TrimRight(rendered, "\n"), "\n")) <= lines {
		return rendered
	}

	rows := splitConversation(rendered)

	// Placed from the end of the conversation rather than around a cursor, so the newest message
	// holds its row as the thread grows and the reader walks back from there.
	place := func(visible int) int {
		return len(rows) - visible - min(t.scroll, len(rows)-visible)
	}

	return paint.WindowRows(rows, lines, place, reviewMarkerStyle())
}

// splitConversation is the conversation's rows once it no longer fits, without the gap that
// parts it from the head, since a clipped thread has no row to spare on it.
func splitConversation(rendered string) []string {
	return strings.Split(strings.Trim(rendered, "\n"), "\n")
}

// detailsFit reports whether the sidebar has a line left over once the record has taken its
// share.
func (r reviewScreen) detailsFit(s panelShared) bool {
	return s.draw.sidebarFitsDetails(r.help, r.headView(s, 0))
}

// detailsRoom is the rows the sidebar has under the head and the legend it is drawing.
func (r reviewScreen) detailsRoom(s panelShared) int {
	return s.draw.sidebarDetailsRoom(r.help, r.headView(s, 0))
}

// headView is the fixed top of the sidebar: the reader's own comments, then the place the cursor
// is standing.
func (r reviewScreen) headView(s panelShared, yoursRoom int) string {
	switch {
	case r.loading:
		return dimStyle.Render(reviewLoadingWord) + "\n"
	case r.diff.GetView() == nil || r.diff.GetView().Len() == 0:
		return dimStyle.Render(reviewEmptyWord) + "\n"
	}

	return r.yoursView(s, yoursRoom) + r.diff.anchorView()
}

// reviewNote is a line of prose under the conversation, wrapped into the sidebar's own column.
func reviewNote(s panelShared, style lipgloss.Style, text string) string {
	var out strings.Builder
	for _, row := range paint.WrapRows(text, lineWidthOf(s.pane)-2) {
		out.WriteString(style.Render("  "+row) + "\n")
	}

	return out.String()
}

func (r reviewScreen) restView(s panelShared) string {
	if r.loading || r.diff.GetView() == nil || r.diff.GetView().Len() == 0 {
		return ""
	}

	if r.threads.composing {
		return "\n" + r.threads.editor.view() + "\n"
	}

	if r.threads.releasing {
		return r.releaseView(s)
	}

	out := "\n" + r.threadView(s)

	// A notice answers the last keypress and a keypress clears it, so what the reader is told
	// about the diff itself stands underneath and comes back the moment the answer is gone.
	if notice := r.notice; notice != "" {
		out += "\n" + reviewNote(s, paint.Panel.Style(reviewNoticeTone), notice)
	} else if since := r.diff.since; since != "" && r.diff.baseLabel == "" {
		out += "\n" + reviewNote(s, dimStyle, since)
	}

	return out
}

// anchorView names the place the cursor is standing, so the sidebar says what a new thread
// would be pinned to without repeating the line the diff already draws beside it.
func (d reviewDiff) anchorView() string {
	anchor, ok := d.GetView().GetAnchor(d.GetCursor())
	if !ok {
		if thread, ok := d.threadAtCursor(); ok {
			return paint.Panel.Title(clipName(diffview.FormatThreadLocation(thread))) +
				d.commentWhereView(thread) + "\n"
		}

		return dimStyle.Render("  move onto a line to start a thread on it") + "\n"
	}

	head := paint.Panel.Title(clipName(diffview.FormatLocation(anchor.FilePath, anchor.OldStart, anchor.NewStart)))
	if thread, ok := d.threadAtCursor(); ok {
		head += d.commentWhereView(thread)
	}

	return head + "\n"
}

// commentWhereView is which comment of how many the cursor is standing on.
func (d reviewDiff) commentWhereView(thread *agentv1.ReviewThread) string {
	at, total := d.GetView().CommentPosition(thread)
	if at == 0 || total < 2 {
		return ""
	}

	return dimStyle.Render(fmt.Sprintf("  %d of %d", at, total))
}

// releaseView asks what to send: an optional word about the work as a whole.
func (r reviewScreen) releaseView(s panelShared) string {
	out := "\n" + r.threads.editor.view() + "\n\n"

	return out + reviewNote(s, dimStyle, keyLabel(formKeys().SendNow)+" send and interrupt"+separated+
		keyLabel(formKeys().Back)+" cancel")
}

func (r reviewScreen) threadView(s panelShared) string {
	thread, ok := r.diff.threadAtCursor()
	if !ok {
		return dimStyle.Render("  no thread here yet") + "\n"
	}

	out := r.originView(s, thread)
	for _, message := range thread.GetMessages() {
		out += r.messageView(s, message)
	}

	return out
}

// originView is where a thread was written and the line it was written against, drawn only once
// that code has moved.
func (r reviewScreen) originView(s panelShared, thread *agentv1.ReviewThread) string {
	if !r.diff.GetView().HasMoved(thread) {
		return ""
	}

	out := dimStyle.Render("  "+r.diff.originSaid(thread)) + "\n"
	for _, quoted := range thread.GetQuotedLines() {
		out += paint.Panel.Style(reviewQuoteTone).Italic(true).Render(reviewWrap(s, "  "+quoted)) + "\n"
	}

	return out + "\n"
}

// originSaid says why a comment is not on the line it was written against, in terms of what
// happened rather than of the placement komrad settled on.
func (d reviewDiff) originSaid(thread *agentv1.ReviewThread) string {
	if _, drawn := d.GetView().GetFile(thread.GetFilePath()); !drawn {
		if d.base != "" {
			return "still in the change, outside what is on screen" + separated + "written against"
		}

		return "the file this was about has gone" + separated + "written against"
	}

	if d.GetView().IsGhosted(thread) {
		return "the code under this has changed" + separated + "written against"
	}

	return "moved since it was written" + separated + "written against"
}

func (r reviewScreen) messageView(s panelShared, message *agentv1.ReviewMessage) string {
	author, tone := "you", reviewHumanTone
	if message.GetAuthorKind() == agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
		author, tone = r.agentName, reviewAgentTone
	}

	head := paint.Panel.Style(tone).Bold(true).Render(author)
	if when := reviewTimeOf(message.GetCreatedAt()); when != "" {
		head += " " + dimStyle.Render(when)
	}

	return head + "\n" + reviewWrap(s, message.GetBody()) + "\n" + reviewDeliveryBadge(paint.Panel, s.glyphs, message) + "\n"
}

// sendDesc says what the send key opens.
func (t reviewThreads) sendDesc() string {
	if t.holdsUnsent() {
		return "send to the agent"
	}

	return "send a note"
}

// details is the record drawn under the review sidebar.
func (r reviewScreen) details(s panelShared) details {
	index := slices.IndexFunc(s.listing.agents, func(item panel.AgentRow) bool { return item.Agent.ID == r.agentID })
	if index < 0 {
		return details{}
	}

	item := s.listing.agents[index]
	agent := item.Agent
	fields := agentDetailFields(s, item)

	// The notices read the row the list would draw, and which columns that row fills depends on
	// whether the agent owns its checkout, which only its workflow says.
	row := agentRow{item: item}
	if workflow := workflowByID(s.listing.workflows, agent.WorkflowID); workflow != nil {
		row.workflow = *workflow
	}

	return details{
		id:      agent.ID,
		notices: agentNotices(s.glyphs, row),
		fields:  append(fields, r.fields()...),
	}
}

// fields are the facts the diff itself carries, which the agent's own record does not hold: what
// it was measured against and what was kept out of it.
func (r reviewScreen) fields() []recordField {
	fields := []recordField{
		{label: "base", value: label.OrDash(git.ShortSHA(r.round.GetBaseSha()))},
		{label: "snapshot", value: label.OrDash(git.ShortSHA(r.round.GetSnapshotSha()))},
	}
	if len(r.exclude) > 0 {
		fields = append(fields, recordField{label: "excludes", value: strings.Join(r.exclude, ", ")})
	}

	return fields
}

// reviewMarkerStyle is what the counts above and below the conversation are drawn in.
func reviewMarkerStyle() lipgloss.Style {
	return paint.Panel.Header()
}

func reviewWrap(s panelShared, body string) string {
	return lipgloss.NewStyle().Width(max(20, s.pane-2)).Render(body)
}

func reviewTimeOf(stamp string) string {
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return ""
	}

	return at.Local().Format("3:04PM")
}

// composeDesc says what the compose key would do where the cursor is standing.
func (r reviewScreen) composeDesc() string {
	if _, ok := r.diff.threadAtCursor(); ok {
		return "reply in this thread"
	}

	if r.diff.isOnChange() {
		return "comment on the change"
	}

	return "comment on this line"
}

// backDesc says which of the two things the back key leaves.
func (r reviewScreen) backDesc() string {
	if r.hold.atEditor {
		return "close the editor"
	}

	return "back to the agents"
}

// reloadDesc says what the reload key would do.
func (r reviewScreen) reloadDesc(s panelShared) string {
	if r.isWorktreeMoved(s) {
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
	if d.GetView() == nil {
		return "fold / whole file"
	}

	// A folded file has nothing on screen to open out, so the expand key is refused there and
	// the legend names the fold key alone.
	file, ok := d.GetView().GetDrawnFileAt(d.GetCursor())

	// Marking a file read is refused in a round, so the row names the key that still works.
	if d.isNarrowed() {
		if ok && d.GetView().IsExpanded(file.Path()) {
			return "show the changes"
		}

		return "show the whole file"
	}

	if ok && d.GetView().IsViewed(file.Path()) {
		return "open the file"
	}

	if ok && d.GetView().IsExpanded(file.Path()) {
		return "fold / the changes"
	}

	return "fold / whole file"
}
