package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"komrad/internal/glyph"
	"komrad/internal/ui/diffview"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	agentv1 "komrad/proto/gen/agent/v1"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The tones the review wears, which the region rendering each one turns into a colour.
const (
	reviewQuoteTone     = paint.ToneMuted
	reviewScopeOnTone   = paint.TonePrimary
	reviewHumanTone     = paint.ToneUserMe
	reviewAgentTone     = paint.ToneUserAgent
	reviewerTone        = paint.ToneUserReviewer
	reviewNoticeTone    = paint.ToneSuccess
	reviewAddedTone     = paint.ToneSuccess
	reviewRemovedTone   = paint.ToneDanger
	reviewReleasedTone  = paint.ToneWarning
	reviewHistoryTone   = paint.ToneWarning
	reviewReadTone      = paint.ToneSuccess
	reviewForgeHeldTone = paint.ToneDanger
)

// reviewChangeFolds is how many ways the change block can be rendered, roomiest first.
const reviewChangeFolds = 2

// view renders the diff and, beside it, the file tree, with the thread the cursor is standing on
// in a popup over them.
func (r reviewScreen) view(m *model) string {
	split, ok := m.screenColumnSplit()
	if !ok && m.term.paneMeasure > 0 {
		return r.sidebarView(m)
	}
	if !ok {
		band := r.bandView(m)
		layout := r.layoutSidebar(m, band)

		return r.overlayThread(m, strings.TrimRight(band+r.renderSidebar(layout, layout.block()), "\n"))
	}

	// The band stands over the diff rather than over the whole window, and what it carries acts on
	// the patch.
	band := r.bandView(m)
	area := band + r.diffView(m, split)
	diff := m.markZone(zoneList, area, split.column)

	return r.overlayThread(m, m.joinColumns(split, r.sidebarView(m), diff))
}

// sidebarView is the sidebar rendered as one half of a split, trimmed so the two halves are the same
// height.
func (r reviewScreen) sidebarView(m *model) string {
	layout := r.layoutSidebar(m)
	block := m.markZone(zoneBlock, layout.block(), lineWidthOf(m.paneWidth()))

	return strings.TrimRight(r.renderSidebar(layout, block), "\n")
}

// overlayThread renders the popup over an area, and leaves the area alone where the popup has
// nothing to hold or the frame is rendered to find its blocks.
func (r reviewScreen) overlayThread(m *model, area string) string {
	if m.isMarkingZones() {
		return area
	}

	inner := r.measurePopupInner(m)
	body, back := r.popupBody(m, inner)
	if m.isPopupMenu() && m.form.isActive() {
		body = withSection(body, m.heldFormView(), inner)
		back = 0
	}
	if body == "" {
		return area
	}

	slot := r.placeThread(m, body)

	return overlayPopup(area, renderPopup(body, back, slot), slot)
}

// renderSidebar is the panel's own column: what the change is, the pull request open from the
// branch, the block the cursor moves through, where the cursor is standing, and what the reader
// was last told.
func (r reviewScreen) renderSidebar(layout sidebarLayout, block string) string {
	column := paint.PadRows(layout.change+block+r.placeView()+layout.notes, layout.spare)

	return layout.bar + column + layout.rule + layout.help
}

// sidebarLayout is the sidebar measured once.
type sidebarLayout struct {
	bar    string
	rule   string
	help   string
	change string
	// side is the stack, the forge, what needs the person and the description.
	side  string
	files string
	notes string
	spare int
}

// block is the side and the file tree, the rows the sidebar's cursor moves through.
func (l sidebarLayout) block() string {
	return l.side + l.files
}

// layoutSidebar gives the column to the file tree, the bar over it shrinking and then giving way
// before the tree loses a row it needs.
func (r reviewScreen) layoutSidebar(m *model, above ...string) sidebarLayout {
	bare := r.layoutUnder(m, "", above)
	for _, bar := range m.viewsBars() {
		if barred := r.layoutUnder(m, bar, above); r.doesFitUnderBar(barred, bare) {
			return barred
		}
	}

	return bare
}

// doesFitUnderBar reports whether a bar costs the sidebar neither its legend nor its file tree's floor.
func (r reviewScreen) doesFitUnderBar(barred, bare sidebarLayout) bool {
	if barred.help != bare.help {
		return false
	}

	if barred.spare >= 0 && paint.CountLines(barred.change+barred.block()+r.placeView()) > barred.spare {
		return false
	}

	return paint.CountLines(barred.files) >= min(paint.CountLines(bare.files), r.countFilesFloor(bare.spare))
}

// layoutUnder measures the sidebar with bar over it.
func (r reviewScreen) layoutUnder(m *model, bar string, above []string) sidebarLayout {
	help := m.renderHelp(r.help(m), slices.Concat(above, []string{r.placeView()})...)
	rule := m.renderRecord(slices.Concat(above, []string{bar, help})...)
	spare := m.listLines(slices.Concat(above, []string{bar, help, rule})...)

	layout := sidebarLayout{
		bar:   bar,
		rule:  rule,
		help:  help,
		notes: r.notesView(m),
		spare: spare,
	}

	if spare < 0 {
		layout.change = r.changeView(m, 0)
		layout.side = r.sideView(m, paint.Unbounded)
		layout.files = r.filesView(m, paint.Unbounded)

		return layout
	}

	floor := r.countFilesFloor(spare)
	room := spare - paint.CountLines(r.placeView())
	layout.change = r.foldChange(m, room-floor)
	room -= paint.CountLines(layout.change)
	layout.side = r.sideView(m, max(room-floor, 0))
	room -= paint.CountLines(layout.side)
	lines := max(room-paint.CountLines(layout.notes), floor)
	layout.files = r.filesView(m, max(min(lines, room), 0))
	layout.notes = cutNotes(layout.notes, room-paint.CountLines(layout.files))

	return layout
}

// foldChange is the change block in the most rows that leave the rows under it their room, else in
// the fewest rows that fit.
func (r reviewScreen) foldChange(m *model, room int) string {
	wanted := paint.CountLines(r.sideView(m, paint.Unbounded))

	var shortest string
	for fold := range reviewChangeFolds {
		change := r.changeView(m, fold)
		if paint.CountLines(change)+wanted <= room {
			return change
		}
		if fold == 0 || paint.CountLines(change) < paint.CountLines(shortest) {
			shortest = change
		}
	}

	if paint.CountLines(shortest) <= room {
		return shortest
	}

	return ""
}

func cutNotes(notes string, rows int) string {
	if rows >= paint.CountLines(notes) {
		return notes
	}

	return strings.Join(strings.SplitAfter(notes, "\n")[:max(rows, 0)], "")
}

// help keeps the strip to what the screen is for.
func (r reviewScreen) help(m *model) keyMap {
	own := r.actions(m).help(m)

	return keyMap{
		short: slices.Concat(own.short, m.globalHelp()),
		full: slices.Concat(
			own.full,
			m.fullGlobalHelp(),
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
func (r reviewScreen) notesView(m *model) string {
	if r.loading || r.diff.View() == nil || r.diff.View().Len() == 0 {
		return ""
	}

	if notice := r.notice; notice != "" {
		return "\n" + reviewNote(lineWidthOf(m.paneWidth()), paint.Panel.Style(reviewNoticeTone), notice)
	}

	if since := r.diff.since; since != "" && r.diff.spec.isWholeChange() {
		return "\n" + reviewNote(lineWidthOf(m.paneWidth()), dimStyle(), since)
	}

	return ""
}

// anchorView names the place the cursor is standing, so the sidebar says what a new thread
// would be pinned to without repeating the line the diff already renders beside it.
func (d reviewDiff) anchorView() string {
	anchor, ok := d.View().GetAnchor(d.Cursor())
	if !ok {
		if thread, ok := d.threadAtCursor(); ok {
			return paint.Panel.Title(clipName(diffview.FormatThreadLocation(thread))) +
				d.commentWhereView(thread) + "\n"
		}

		return dimStyle().Render("  move onto a line to start a thread on it") + "\n"
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

	return dimStyle().Render(fmt.Sprintf("  %d of %d", at, total))
}

// threadView is a thread's conversation wrapped to the width given, oldest message first.
func (r reviewScreen) threadView(m *model, thread *agentv1.ReviewThread, width int) string {
	stamp := r.diff.laterStamp(thread)
	answered := findAnswer(thread)
	later := false

	out := r.originView(thread, width)
	for i, message := range thread.GetMessages() {
		if i > 0 && !later && stamp != "" && !doesPredate(message.GetCreatedAt(), stamp) {
			later = true
			out += "\n" + dimStyle().Render(glyph.Rule+glyph.Rule+" later, after this code changed "+glyph.Rule+glyph.Rule) + "\n"
		}

		note := heldForgeSaid(thread, message)
		if i == answered {
			note = r.diff.addressedSaid(message)
		}

		out += r.messageView(m, thread, message, messageViewParams{
			note:  note,
			width: width,
		})
	}

	return out + r.forgeRepliesView(m, thread, width)
}

// originView is where a thread was written and the line it was written against, rendered only once
// that code has moved.
func (r reviewScreen) originView(thread *agentv1.ReviewThread, width int) string {
	if diffview.IsWholeChangeThread(thread) || !r.diff.View().HasMoved(thread) {
		return ""
	}

	said := r.diff.originSaid(thread)
	if len(thread.GetQuotedLines()) > 0 {
		said += separated + "written against"
	}

	out := dimStyle().Render("  "+said) + "\n"
	for _, quoted := range thread.GetQuotedLines() {
		out += paint.Panel.Style(reviewQuoteTone).Italic(true).Render(reviewWrap(width, "  "+quoted)) + "\n"
	}

	return out + "\n"
}

// originSaid says why a comment is not on the line it was written against, in terms of what
// happened rather than of the placement komrad settled on.
func (d reviewDiff) originSaid(thread *agentv1.ReviewThread) string {
	if _, rendered := d.View().GetFile(thread.GetFilePath()); !rendered {
		if d.spec.base != "" {
			return "still in the change, outside what is on screen"
		}

		return "the file this was about has gone"
	}

	if d.View().IsGhosted(thread) {
		return "the code under this has changed"
	}

	return "moved since it was written"
}

// messageViewParams is what is said beside a message and the width it is wrapped to.
type messageViewParams struct {
	note  string
	width int
}

// messageView is one message: who wrote it and when, what it says, and where it stands with the
// writer.
func (r reviewScreen) messageView(m *model, thread *agentv1.ReviewThread, message *agentv1.ReviewMessage, params messageViewParams) string {
	author, tone := r.authorOf(message)
	head := renderCommentHead(tone, author, message.GetCreatedAt())
	if params.note != "" {
		head += "   " + dimStyle().Render(params.note)
	}

	body := message.GetBody()
	if message.GetForge() != nil {
		head = forgeMark(m.glyphs) + head
		body = plainForgeText(body)
	}

	return head + "\n" + reviewWrap(params.width, body) + "\n" + r.reviewDeliveryBadge(m, thread, message) + "\n"
}

func (r reviewScreen) authorOf(message *agentv1.ReviewMessage) (string, paint.Tone) {
	if forge := message.GetForge(); forge != nil {
		return describeForgeLogin(forge.GetLogin()), diffview.ForgeTone
	}

	if message.GetAuthorKind() != agentv1.ReviewAuthorKind_REVIEW_AUTHOR_KIND_AGENT {
		return "you", reviewHumanTone
	}

	if message.GetAuthorAgentId() == r.agentID {
		return "writer", reviewAgentTone
	}

	return "reviewer", reviewerTone
}

func renderCommentHead(tone paint.Tone, author, writtenAt string) string {
	head := paint.Panel.Style(tone).Bold(true).Render(author)
	if when := reviewTimeOf(writtenAt); when != "" {
		head += " " + dimStyle().Render(when)
	}

	return head
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

	return "comment on this line"
}

// backDesc says which of the two things the back key leaves.
func (r reviewScreen) backDesc() string {
	if r.hold.atEditor {
		return "close the editor"
	}

	return "back to the agents"
}

func (r reviewScreen) diffBackDesc(m *model) string {
	if r.hold.isHeld() || !r.isBlockRendered(m) {
		return r.backDesc()
	}

	return "back to the files"
}

// reloadDesc says what the reload key would do.
func (r reviewScreen) reloadDesc(m *model) string {
	if r.isWorktreeMoved(m) {
		return "catch up"
	}

	return "read the diff again"
}

// agentThreadsDesc says which way the agent threads key would go.
func (d reviewDiff) agentThreadsDesc() string {
	if d.hidesAgentThreads {
		return "show agents' conversations"
	}

	return "hide agents' conversations"
}

// resolveDesc says which way the settle key would go where the cursor is standing.
func (d reviewDiff) resolveDesc() string {
	if thread, ok := d.threadAtCursor(); ok && thread.GetResolvedAt() != "" {
		return "reopen the thread"
	}

	return "settle the thread"
}

// viewedDesc names what the viewed key does to the file the cursor is in.
func (d reviewDiff) viewedDesc() string {
	if d.View() == nil {
		return "mark the file viewed"
	}

	if file, ok := d.View().GetPatchFileAt(d.Cursor()); ok && d.View().IsViewed(file.Path()) {
		return "open the file"
	}

	return "mark the file viewed"
}

// changeView is what the sidebar says about the change under review, which a higher fold joins
// onto fewer rows before they wrap.
func (r reviewScreen) changeView(m *model, fold int) string {
	stat := r.diff.statView()

	var rows []string
	if fold == 0 {
		rows = []string{r.agentName, stat}
	} else {
		rows = []string{joinViews(r.agentName, stat)}
	}

	width := lineWidthOf(m.paneWidth())

	var out strings.Builder
	for _, row := range rows {
		if row != "" {
			out.WriteString(ansi.Wrap(row, width, "") + "\n")
		}
	}
	if out.Len() == 0 {
		return ""
	}

	return out.String() + "\n"
}

// joinViews strings the parts that say something together on one row.
func joinViews(parts ...string) string {
	said := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			said = append(said, part)
		}
	}

	return strings.Join(said, dimStyle().Render(separated))
}

// statView is how big the change is, with the counts coloured the way the lines themselves are.
func (d reviewDiff) statView() string {
	if d.View() == nil {
		return ""
	}

	stat := d.View().Stat()
	if stat.Files == 0 {
		return ""
	}

	out := dimStyle().Render(label.Counted(stat.Files, "file")+", ") +
		paint.Panel.Style(reviewAddedTone).Render(fmt.Sprintf("+%d", stat.Added)) + " " +
		paint.Panel.Style(reviewRemovedTone).Render(fmt.Sprintf("-%d", stat.Removed))

	if read, total := d.View().CountViewedFiles(); read > 0 {
		out += dimStyle().Render(fmt.Sprintf("  %d/%d viewed", read, total))
	}

	return out
}
