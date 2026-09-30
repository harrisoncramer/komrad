package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	agentv1 "komrad/proto/gen/agent/v1"

	"charm.land/lipgloss/v2"
)

// The tones the review wears, which the region drawing each one turns into a colour.
const (
	reviewQuoteTone  = paint.ToneMuted
	reviewHumanTone  = paint.ToneSelf
	reviewAgentTone  = paint.ToneOther
	reviewerTone     = paint.ToneThird
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

// view draws the diff and, beside it, the file tree, with the thread the cursor is standing on
// in a popup over the diff.
func (r reviewScreen) view(s panelShared) string {
	split, ok := s.draw.screenColumnSplit()
	if !ok {
		band := r.bandView(s, s.pane)
		layout := r.layoutSidebar(s, band)
		column := strings.TrimRight(band+r.drawSidebar(layout), "\n")
		cursor := paint.CountLines(band+layout.bar+layout.files+layout.offers+r.placeView()) - 1

		return r.overlayThread(s, column, cursor, 0)
	}

	// Trimmed so the two halves are the same height.
	sidebar := strings.TrimRight(r.drawSidebar(r.layoutSidebar(s)), "\n")

	// The band stands over the diff rather than over the whole window, and what it carries acts on
	// the patch.
	band := r.bandView(s, split.column)
	area := r.overlayThread(s, band+r.diffView(s, split), strings.Count(band, "\n")+r.diff.GetCursorRow(split.rows), reviewPopupLeft)
	diff := s.draw.markZone(zoneList, area, split.column)

	return s.draw.joinColumns(split, sidebar, diff)
}

// overlayThread draws the popup over an area beside the cursor's row, and leaves the area alone
// where the popup has nothing to hold.
func (r reviewScreen) overlayThread(s panelShared, area string, cursor, left int) string {
	body := r.popupBody(s, measurePopupInner(s))
	if body == "" {
		return area
	}

	rows := paint.CountLines(area)
	width := lipgloss.Width(area)
	box := drawPopup(body, measurePopupWidth(width-left), min(reviewPopupHeight, rows))

	return overlayPopup(area, width, placePopup(box, cursor, rows, left))
}

// drawSidebar is the panel's own column: the files the change touches, where the cursor is
// standing, and what the reader was last told.
func (r reviewScreen) drawSidebar(layout sidebarLayout) string {
	column := paint.PadRows(layout.files+layout.offers+r.placeView()+layout.notes, layout.spare)

	return layout.bar + column + layout.rule + layout.help
}

// sidebarLayout is the sidebar measured once.
type sidebarLayout struct {
	bar    string
	rule   string
	help   string
	files  string
	offers string
	notes  string
	spare  int
}

// layoutSidebar gives the column to the file tree, the bar over it giving way before the tree
// loses a row it needs.
func (r reviewScreen) layoutSidebar(s panelShared, above ...string) sidebarLayout {
	bare := r.layoutUnder(s, "", above)
	barred := r.layoutUnder(s, s.draw.viewsBar(), above)

	if barred.help != bare.help {
		return bare
	}

	if barred.spare >= 0 && paint.CountLines(barred.files+barred.offers+r.placeView()) > barred.spare {
		return bare
	}

	if paint.CountLines(barred.files) < min(paint.CountLines(bare.files), r.countFilesFloor(bare.spare)) {
		return bare
	}

	return barred
}

// layoutUnder measures the sidebar with bar over it.
func (r reviewScreen) layoutUnder(s panelShared, bar string, above []string) sidebarLayout {
	help := s.draw.renderHelp(r.help(s), slices.Concat(above, []string{r.placeView()})...)
	rule := s.draw.drawRecord(slices.Concat(above, []string{bar, help})...)
	spare := s.draw.listLines(slices.Concat(above, []string{bar, help, rule})...)

	layout := sidebarLayout{
		bar:   bar,
		rule:  rule,
		help:  help,
		notes: r.notesView(s),
		spare: spare,
	}

	if spare < 0 {
		layout.files = r.filesView(s, paint.Unbounded)
		layout.offers = r.offersView(s, offerRows)

		return layout
	}

	layout.offers = r.offersView(s, countOffersRoom(spare, r.countFilesFloor(spare)))
	room := spare - paint.CountLines(layout.offers) - paint.CountLines(r.placeView())
	lines := max(room-paint.CountLines(layout.notes), r.countFilesFloor(spare))
	layout.files = r.filesView(s, max(min(lines, room), 0))
	layout.notes = cutNotes(layout.notes, room-paint.CountLines(layout.files))

	return layout
}

// cutNotes keeps the first rows of the notes that fit.
func cutNotes(notes string, rows int) string {
	if rows >= paint.CountLines(notes) {
		return notes
	}

	return strings.Join(strings.SplitAfter(notes, "\n")[:max(rows, 0)], "")
}

// help keeps the strip to what the screen is for.
func (r reviewScreen) help(s panelShared) keyMap {
	own := r.actions().help(s)

	return keyMap{
		short: slices.Concat(own.short, s.draw.globalHelp()),
		full: slices.Concat(
			own.full,
			s.draw.fullGlobalHelp(),
		),
	}
}

// placeView is the blank row under the file tree and the place the cursor is standing.
func (r reviewScreen) placeView() string {
	if r.loading || r.diff.View() == nil || r.diff.View().Len() == 0 {
		return ""
	}

	return "\n" + r.diff.anchorView()
}

// reviewNote is a line of prose wrapped to the width given.
func reviewNote(width int, style lipgloss.Style, text string) string {
	var out strings.Builder
	for _, row := range paint.WrapRows(text, width-2) {
		out.WriteString(style.Render("  "+row) + "\n")
	}

	return out.String()
}

// notesView is what the reader was last told under the place the cursor is standing.
func (r reviewScreen) notesView(s panelShared) string {
	if r.loading || r.diff.View() == nil || r.diff.View().Len() == 0 {
		return ""
	}

	// A notice answers the last keypress and a keypress clears it, so what the reader is told
	// about the diff itself stands underneath and comes back the moment the answer is gone.
	if notice := r.notice; notice != "" {
		return "\n" + reviewNote(lineWidthOf(s.pane), paint.Panel.Style(reviewNoticeTone), notice)
	}

	if since := r.diff.since; since != "" && r.diff.baseLabel == "" {
		return "\n" + reviewNote(lineWidthOf(s.pane), dimStyle, since)
	}

	return ""
}

// anchorView names the place the cursor is standing, so the sidebar says what a new thread
// would be pinned to without repeating the line the diff already draws beside it.
func (d reviewDiff) anchorView() string {
	anchor, ok := d.View().GetAnchor(d.Cursor())
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
	at, total := d.View().CommentPosition(thread)
	if at == 0 || total < 2 {
		return ""
	}

	return dimStyle.Render(fmt.Sprintf("  %d of %d", at, total))
}

// releaseView asks what to send: an optional word about the work as a whole.
func (r reviewScreen) releaseView(width int) string {
	out := r.threads.editor.view() + "\n\n"

	return out + reviewNote(width, dimStyle, keyLabel(formKeys().SendNow)+" send and interrupt"+separated+
		keyLabel(formKeys().Back)+" cancel")
}

// threadView is a thread's conversation wrapped to the width given, oldest message first.
func (r reviewScreen) threadView(s panelShared, thread *agentv1.ReviewThread, width int) string {
	stamp := r.diff.laterStamp(thread)
	answered := findAnswer(thread)
	later := false

	out := r.originView(thread, width)
	for i, message := range thread.GetMessages() {
		if i > 0 && !later && stamp != "" && !doesPredate(message.GetCreatedAt(), stamp) {
			later = true
			out += "\n" + dimStyle.Render(glyph.Rule+glyph.Rule+" later, after this code changed "+glyph.Rule+glyph.Rule) + "\n"
		}

		note := ""
		if i == answered {
			note = r.diff.addressedSaid(message)
		}

		out += r.messageView(s, message, messageViewParams{
			note:  note,
			width: width,
		})
	}

	return out
}

// originView is where a thread was written and the line it was written against, drawn only once
// that code has moved.
func (r reviewScreen) originView(thread *agentv1.ReviewThread, width int) string {
	if !r.diff.View().HasMoved(thread) {
		return ""
	}

	out := dimStyle.Render("  "+r.diff.originSaid(thread)) + "\n"
	for _, quoted := range thread.GetQuotedLines() {
		out += paint.Panel.Style(reviewQuoteTone).Italic(true).Render(reviewWrap(width, "  "+quoted)) + "\n"
	}

	return out + "\n"
}

// originSaid says why a comment is not on the line it was written against, in terms of what
// happened rather than of the placement komrad settled on.
func (d reviewDiff) originSaid(thread *agentv1.ReviewThread) string {
	if _, drawn := d.View().GetFile(thread.GetFilePath()); !drawn {
		if d.base != "" {
			return "still in the change, outside what is on screen" + separated + "written against"
		}

		return "the file this was about has gone" + separated + "written against"
	}

	if d.View().IsGhosted(thread) {
		return "the code under this has changed" + separated + "written against"
	}

	return "moved since it was written" + separated + "written against"
}

// messageViewParams is what is said beside a message and the width it is wrapped to.
type messageViewParams struct {
	note  string
	width int
}

// messageView is one message: who wrote it and when, what it says, and how far it got.
func (r reviewScreen) messageView(s panelShared, message *agentv1.ReviewMessage, params messageViewParams) string {
	author, tone := r.authorOf(message)
	head := paint.Panel.Style(tone).Bold(true).Render(author)
	if when := reviewTimeOf(message.GetCreatedAt()); when != "" {
		head += " " + dimStyle.Render(when)
	}
	if params.note != "" {
		head += "   " + dimStyle.Render(params.note)
	}

	return head + "\n" + reviewWrap(params.width, message.GetBody()) + "\n" + reviewDeliveryBadge(paint.Panel, s.glyphs, message) + "\n"
}

func (r reviewScreen) authorOf(message *agentv1.ReviewMessage) (string, paint.Tone) {
	if message.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
		return "you", reviewHumanTone
	}

	if message.GetAuthorAgentId() == r.agentID {
		return "implementer", reviewAgentTone
	}

	return "reviewer", reviewerTone
}

// sendDesc says what the send key opens.
func (t reviewThreads) sendDesc() string {
	if t.holdsUnsent() {
		return "send to the agent"
	}

	return "send a note"
}

func reviewWrap(width int, body string) string {
	return lipgloss.NewStyle().Width(max(width, 1)).Render(body)
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
	if d.View() == nil {
		return "fold / whole file"
	}

	// A folded file has nothing on screen to open out, so the expand key is refused there and
	// the legend names the fold key alone.
	file, ok := d.View().GetDrawnFileAt(d.Cursor())

	// Marking a file read is refused in a round, so the row names the key that still works.
	if d.isNarrowed() {
		if ok && d.View().IsExpanded(file.Path()) {
			return "show the changes"
		}

		return "show the whole file"
	}

	if ok && d.View().IsViewed(file.Path()) {
		return "open the file"
	}

	if ok && d.View().IsExpanded(file.Path()) {
		return "fold / the changes"
	}

	return "fold / whole file"
}
