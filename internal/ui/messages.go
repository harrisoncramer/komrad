package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"weave/lib/dates"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// messagesRows is the most rows the region takes, which the pane keeps back whether or not the
// message on screen fills them.
const messagesRows = 3

// messagesRegionRows is what the region costs the pane: its rows and the rule under them.
const messagesRegionRows = messagesRows + 1

// messagesHeaderRows is what the title, the blank under it, and the filter take above the list.
const messagesHeaderRows = 3

// messagesListFloor is how many rows the list keeps once the region has taken its own.
const messagesListFloor = 2

// messagesIndent is the gutter every row of a message is drawn behind, which is the cursor's.
const messagesIndent = 2

// messagesTallyWidth is the room held on the right of the summary for the count beside it.
const messagesTallyWidth = 8

// messagesFocusKey takes the region and hands it back, and is not tab, which huh binds as the
// key moving between a form's fields.
const messagesFocusKey = "ctrl+g"

// messagesMockKey pushes a fixture into the stack, which is how the region is looked at without
// making the panel refuse something first.
const messagesMockKey = "M"

// messagesCap is how far back the stack goes.
const messagesCap = 50

// messagesPulseBlinks is how many times the summary of a message blinks as it lands.
const messagesPulseBlinks = 5

type messageKind int

const (
	messageInfo messageKind = iota
	messageSuccess
	messageWarning
	messageError
)

type message struct {
	kind    messageKind
	summary string
	detail  string
	hint    string
	trace   string
	at      time.Time
}

type messagesState struct {
	items   []message
	cursor  int
	focused bool
	// field is the piece of the message the cursor stands on, and flashed the one a copy has just
	// taken.
	field   int
	flashed int
	// landed counts the messages pushed, and pulse blinks the newest of them as it arrives.
	landed int
	pulse  pulse
}

// pulseMessages blinks the summary when the stack has grown since the count it is given.
func (m model) pulseMessages(before int) (model, tea.Cmd) {
	if m.messages.landed == before {
		return m, nil
	}

	return m, m.startPulse(&m.messages.pulse, messagesPulseBlinks)
}

// messagesPulseOn reports whether the newest message is in the lit half of a blink, which is not
// drawn on an older message the reader has walked back to.
func (m model) messagesPulseOn() bool {
	return m.messages.pulse.lit() && m.messagesCursor() == 0
}

// messagePiece is one part of a message the cursor stands on: the label it is drawn behind, the
// text itself, and the value y copies.
type messagePiece struct {
	label string
	text  string
	value string
	style lipgloss.Style
	// head marks the summary, which is the one piece drawn as a row of its own.
	head bool
}

// messagesRegion is the region as it is drawn under the record, given the record and the legend
// it sits between so it can tell whether the pane has rows to spare for it.
func (m model) messagesRegion(record, help string) string {
	rows, ok := m.messagesFit(record, help)
	if !ok {
		return ""
	}

	slot := strings.Join(m.messagesSlot(), "\n") + "\n"
	if m.isMessagesFocused() {
		// The summary keeps its row while the pieces under it move, so what failed and where in
		// the stack it sits are on screen wherever the cursor has got to.
		drawn := m.messagesRender()
		slot = drawn.rows[0] + "\n" + padRows(renderList(drawn.rows[1:], drawn.cursorRow-1, rows-1), rows-1)
	}

	block := slot
	if record == "" {
		block = m.detailsRule() + block
	}

	return block + m.detailsRule()
}

// messagesFit is how many rows the region takes, refused when they would leave the list too
// short to be worth reading.
func (m model) messagesFit(record, help string) (int, bool) {
	if m.height <= 0 || !m.isMessagesRegion() {
		return 0, false
	}

	spare := m.spareLines(record, help) - messagesHeaderRows - messagesRegionRows
	if spare < messagesListFloor {
		return 0, false
	}

	if !m.isMessagesFocused() {
		return messagesRows, true
	}

	return min(len(m.messagesRender().rows), messagesRows+spare-messagesListFloor), true
}

// messagesRender is the expanded message as its rows, carrying the row the cursor stands on so
// the window follows it.
type messagesRender struct {
	rows      []string
	cursorRow int
}

// messagesRender draws the whole message at the width of the pane, which is what the region
// holds once it has the keys.
func (m model) messagesRender() messagesRender {
	pieces := m.messagePieces()
	if len(pieces) == 0 {
		return messagesRender{rows: m.messagesSlot()}
	}

	cursor := m.messagesPiece()

	drawn := messagesRender{}
	for i, piece := range pieces {
		if i > 0 {
			drawn.rows = append(drawn.rows, "")
		}

		if i == cursor {
			drawn.cursorRow = len(drawn.rows)
		}

		drawn.rows = append(drawn.rows, m.messagePieceRows(piece, i == cursor, m.messages.flashed == i+1)...)
	}

	return drawn
}

// messagePieces are the parts of the message the cursor moves between, each of them a value y
// copies: what failed, the reason, what to do about it, the trace id, and when it landed.
func (m model) messagePieces() []messagePiece {
	if len(m.messages.items) == 0 {
		return nil
	}

	held := m.messages.items[m.messagesCursor()]
	landed := dates.Friendly(held.at)

	// The reason is the one thing here somebody has to read, so it is drawn in the plain
	// foreground and everything around it is dim.
	pieces := []messagePiece{
		{text: held.summary, value: held.summary, head: true},
		{text: held.detail, value: held.detail, style: lipgloss.NewStyle()},
		{text: held.hint, value: held.hint, style: dimStyle},
		{label: "weave trace ", text: held.trace, value: held.trace, style: dimStyle},
		{label: kindLabel(held.kind) + " · ", text: landed, value: landed, style: dimStyle},
	}

	return slices.DeleteFunc(pieces, func(piece messagePiece) bool { return piece.text == "" })
}

// messagePieceRows is one piece wrapped to the pane, the cursor and the label on the first of
// its rows and the rest under the indent the cursor leaves.
func (m model) messagePieceRows(piece messagePiece, hovered, flashed bool) []string {
	if piece.head {
		return []string{m.messagesCursorPrefix(hovered) + m.messagesSummaryStyle(m.messages.items[m.messagesCursor()], messagesIndent, flashed) + m.messagesTally(messagesIndent)}
	}

	style := piece.style
	if flashed {
		style = flashFieldStyle
	}

	rows := []string{}
	for line := range strings.SplitSeq(ansi.Wordwrap(piece.text, max(m.lineWidth()-messagesIndent-2-lipgloss.Width(piece.label), 1), ""), "\n") {
		if len(rows) == 0 {
			label := ""
			if piece.label != "" {
				label = dimStyle.Render(piece.label)
			}

			rows = append(rows, m.messagesCursorPrefix(hovered)+label+style.Render(line))

			continue
		}

		rows = append(rows, strings.Repeat(" ", messagesIndent)+style.Render(line))
	}

	return rows
}

// messagesCursorPrefix is the gutter the cursor stands in, which is the record's.
func (m model) messagesCursorPrefix(hovered bool) string {
	if !hovered {
		return strings.Repeat(" ", messagesIndent)
	}

	return m.cursorPrefix(true)
}

// messagesPiece is the piece the cursor stands on, kept inside the pieces this message has.
func (m model) messagesPiece() int {
	return clampCursor(m.messages.field, len(m.messagePieces()))
}

// copyMessagePiece puts the piece under the cursor on the clipboard, flashing it the way the
// record flashes a field it has copied.
func (m model) copyMessagePiece() (tea.Model, tea.Cmd) {
	pieces := m.messagePieces()
	if len(pieces) == 0 {
		return m, nil
	}

	cursor := m.messagesPiece()
	m.messages.flashed = cursor + 1

	return m, tea.Batch(m.copyCmd(pieces[cursor].value), flashRevertTick())
}

// messagesSlot is the message the cursor is on said in the rows the region always holds: what
// happened, as much of the reason as one line carries, and the way into the rest.
func (m model) messagesSlot() []string {
	if len(m.messages.items) == 0 {
		return []string{
			m.messagesSummaryStyle(message{kind: messageSuccess}, 0, false) + m.messagesTally(0),
			dimStyle.Render("  Weave is running normally"),
		}
	}

	held := m.messages.items[m.messagesCursor()]

	return []string{
		m.messagesSummaryStyle(held, 0, false) + m.messagesTally(0),
		dimStyle.Render("  " + clip(held.detail, max(m.lineWidth()-messagesIndent, 1))),
		dimStyle.Render("  " + m.messagesHint()),
	}
}

// messagesSummaryLine is the summary row's text, clipped short of the tally so the count sits at
// the far side.
func (m model) messagesSummaryLine(taken int) string {
	if len(m.messages.items) == 0 {
		return kindIcon(messageSuccess) + " No messages"
	}

	held := m.messages.items[m.messagesCursor()]

	return clip(kindIcon(held.kind)+" "+held.summary, max(m.lineWidth()-taken-messagesTallyWidth, 1))
}

// messagesSummaryStyle is the summary row drawn with the kind on the icon alone.
func (m model) messagesSummaryStyle(held message, taken int, flashed bool) string {
	line := m.messagesSummaryLine(taken)
	icon, words, _ := strings.Cut(line, " ")

	style := lipgloss.NewStyle()
	if m.messages.focused {
		style = style.Bold(true)
	}
	if flashed || m.messagesPulseOn() {
		style = flashFieldStyle
	}

	return kindStyle(held.kind).Render(icon) + " " + style.Render(words)
}

func (m model) messagesHint() string {
	return messagesFocusKey + " to read"
}

// messagesTally is the count on the right of the summary, which says how much is behind this
// message while the list has the keys, and where in the stack the reader is once it does not.
func (m model) messagesTally(taken int) string {
	count := len(m.messages.items)
	if count == 0 {
		return ""
	}

	tally := fmt.Sprintf("+%d", count-1)
	if m.messages.focused {
		// Counted from the oldest, so the newest message is the last of them rather than the
		// first: the stack itself is held newest first.
		tally = fmt.Sprintf("%d of %d", count-m.messagesCursor(), count)
	} else if count < 2 {
		return ""
	}

	pad := max(m.lineWidth()-taken-lipgloss.Width(m.messagesSummaryLine(taken))-len(tally), 1)

	// The tally is where the region says the keys are its own.
	style := dimStyle
	if m.messages.focused {
		style = headerStyle
	}

	return strings.Repeat(" ", pad) + style.Render(tally)
}

// pushMessage puts a message at the front of the stack and keeps the reader on whatever they
// were reading when it lands.
func (m *model) pushMessage(next message) {
	if next.detail == "" {
		next.detail = "no reason given"
	}

	if next.at.IsZero() {
		next.at = time.Now()
	}

	m.messages.landed++
	m.messages.items = append([]message{next}, m.messages.items...)
	if len(m.messages.items) > messagesCap {
		m.messages.items = m.messages.items[:messagesCap]
	}

	if m.messages.focused {
		m.messages.cursor = m.messagesCursor() + 1

		return
	}

	m.messages.cursor = 0
}

// setNewestHint says what to do about the message that has just been reported, which the write
// that reported it knows and the call that failed does not.
func (m *model) setNewestHint(hint string) {
	if len(m.messages.items) == 0 {
		return
	}

	m.messages.items[0].hint = hint
}

// messagesDoc is the whole of the message the cursor is on, drawn in the block beside the list so
// the reason is read at the width of the window.
func (m model) messagesDoc() string {
	if len(m.messages.items) == 0 {
		return "_No messages_\n\nWeave is running normally."
	}

	held := m.messages.items[m.messagesCursor()]

	var b strings.Builder
	fmt.Fprintf(&b, "`%s` **%s**\n\n%s\n", kindIcon(held.kind), held.summary, held.detail)

	if held.hint != "" {
		fmt.Fprintf(&b, "\n%s\n", held.hint)
	}

	fmt.Fprintf(&b, "\n_%s · %s · message %d of %d_\n", kindLabel(held.kind), held.at.Format("3:04pm"), len(m.messages.items)-m.messagesCursor(), len(m.messages.items))

	if held.trace != "" {
		fmt.Fprintf(&b, "\n```\nweave trace %s\n```\n", held.trace)
	}

	return b.String()
}

// messagesBlock is the message drawn in the block beside the list, asked for directly.
func (m model) messagesBlock(split columnSplit) docWindow {
	return docWindow{
		rows:    docStyledRows(m.messagesDoc(), split.column, kindStyleConfig(m.messagesKind())),
		top:     m.column.top,
		markers: true,
	}
}

func (m model) messagesKind() messageKind {
	if len(m.messages.items) == 0 {
		return messageSuccess
	}

	return m.messages.items[m.messagesCursor()].kind
}

func (m model) messagesCursor() int {
	return clampCursor(m.messages.cursor, len(m.messages.items))
}

// isMessagesRegion is whether the screen on top draws the region at all, which the review screen
// does not.
func (m model) isMessagesRegion() bool {
	// A review mid-input owns every key, so the region gives up its own rather than taking a
	// character out of a comment being typed.
	if m.screen == screenReview && m.review.doesTakeEveryKey() {
		return false
	}

	return m.isDetailsScreen()
}

// isMessagesFocused is whether the region holds the keys, which it cannot on a screen that does
// not draw it.
func (m model) isMessagesFocused() bool {
	return m.messages.focused && m.isMessagesRegion()
}

// toggleMessagesFocus moves the keys between the list and the region, which is also what puts
// the message in the block beside the list.
func (m model) toggleMessagesFocus() model {
	if !m.messages.focused && len(m.messages.items) == 0 {
		return m
	}

	m.messages.focused = !m.messages.focused
	m.details.focused = false
	m.messages.cursor = 0
	m = m.landedOnMessage()

	m.reconcileSolo()
	m.restageScreen()

	return m
}

func (m model) updateMessages(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	// The block beside a form is the one message this region does not draw itself, so it keeps
	// the scroll keys it is read with.
	if direction, ok := m.isColumnScroll(k); ok && m.form.isOpening() {
		return m.scrollColumn(direction), nil
	}

	switch {
	case k == messagesFocusKey:
		return m.toggleMessagesFocus(), nil
	case k == "esc":
		return m.dismissMessage(), nil
	// The newest message is the one on screen, so back through the stack is back through time.
	case k == "<", k == "[":
		m.messages.cursor = clampCursor(m.messagesCursor()+1, len(m.messages.items))
		m = m.landedOnMessage()
	case k == ">", k == "]":
		m.messages.cursor = clampCursor(m.messagesCursor()-1, len(m.messages.items))
		m = m.landedOnMessage()
	case k == messagesMockKey:
		return m.pushMockMessage(), nil
	case isBound(k, m.keys.Details.Copy):
		return m.copyMessagePiece()
	case isBound(k, m.keys.Nav.Up):
		m.messages.field = clampCursor(m.messagesPiece()-1, len(m.messagePieces()))
	case isBound(k, m.keys.Nav.Down):
		m.messages.field = clampCursor(m.messagesPiece()+1, len(m.messagePieces()))
	case isBound(k, m.keys.Nav.Top):
		m.messages.field = 0
	case isBound(k, m.keys.Nav.Bottom):
		m.messages.field = len(m.messagePieces()) - 1
	}

	return m, nil
}

// landedOnMessage puts the cursor back on the top of a message the reader has just walked to.
func (m model) landedOnMessage() model {
	m.messages.field = 0
	m.column.top = 0

	return m
}

// dismissMessage drops the message on screen and lands on the one behind it, handing the keys
// back to the list once the stack is empty.
func (m model) dismissMessage() model {
	if len(m.messages.items) == 0 {
		return m.toggleMessagesFocus()
	}

	cursor := m.messagesCursor()
	m.messages.items = slices.Delete(slices.Clone(m.messages.items), cursor, cursor+1)
	m.messages.cursor = clampCursor(cursor, len(m.messages.items))
	m = m.landedOnMessage()

	if len(m.messages.items) == 0 {
		return m.toggleMessagesFocus()
	}

	m.restageScreen()

	return m
}

// pushMockMessage fills the region from the fixtures.
func (m model) pushMockMessage() model {
	m.pushMessage(mockMessages[len(m.messages.items)%len(mockMessages)])
	m = m.landedOnMessage()
	m.restageScreen()

	return m
}

func (m model) messagesHelp() keyMap {
	copyPiece := binding(m.keys.Details.Copy, "copy")
	history := binding([]string{"<", ">"}, "history")
	dismiss := binding([]string{"esc"}, "dismiss")
	back := binding([]string{messagesFocusKey}, "back to list")

	return keyMap{
		short: []key.Binding{copyPiece, history, dismiss, back, m.helpBinding()},
		full:  []key.Binding{copyPiece, history, dismiss, back, m.moveBinding(), m.helpBinding()},
	}
}

// messagesBinding is the region's own key as the legend names it, which says what pressing it
// does from where the reader is rather than what the region is called.
func (m model) messagesBinding() key.Binding {
	desc := "messages"
	if m.isMessagesFocused() {
		desc = "back to the list"
	}

	return binding([]string{messagesFocusKey}, desc)
}

func kindIcon(kind messageKind) string {
	switch kind {
	case messageError:
		return "✗"
	case messageWarning:
		return "!"
	case messageSuccess:
		return "✔"
	case messageInfo:
		return "·"
	}

	return "·"
}

func kindLabel(kind messageKind) string {
	switch kind {
	case messageError:
		return "error"
	case messageWarning:
		return "warning"
	case messageSuccess:
		return "done"
	case messageInfo:
		return "info"
	}

	return "info"
}

func kindColor(kind messageKind) lipgloss.Color {
	switch kind {
	case messageError:
		return colorDanger
	case messageWarning:
		return colorWarning
	case messageSuccess:
		return colorSuccess
	case messageInfo:
		return colorMuted
	}

	return colorMuted
}

func kindStyle(kind messageKind) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(kindColor(kind))
}

// mockMessages are the fixtures the region is drawn from while nothing real reports through it,
// long enough in the reason to show what the rows do with one that does not fit.
var mockMessages = []message{
	{
		kind:    messageError,
		summary: "Nothing to review in fix-ci",
		detail:  "the worktree holds no change against the base, so there is nothing to open a round on. commit first, then ask again.",
		trace:   "7c1f2a94",
	},
	{
		kind:    messageWarning,
		summary: "Pane died: axle-idempotency",
		detail:  "the harness exited 1 before the first turn finished, and the pane it was running in is gone. read the run log with l.",
		trace:   "91be40c2",
	},
	{
		kind:    messageInfo,
		summary: "Archived 4 agents",
		detail:  "weave-bughunt-job, iss-408-reviewer, test-2 and test are hidden from the sidebar. restore them from the archive screen with Z.",
	},
}
