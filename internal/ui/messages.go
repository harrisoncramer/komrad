package ui

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
}

// messagesRows is what the region takes under the record, drawn whether or not there is anything
// to say, so a message arriving moves no row of the list above it.
const messagesRows = 3

// messagesRegionRows is what the region costs the pane: its rows and the rule under them.
const messagesRegionRows = messagesRows + 1

// messagesFocusKey takes the region and hands it back, and is not tab because huh binds tab as
// the key that moves between a form's fields.
const messagesFocusKey = "ctrl+g"

// messagesMockKey pushes a fixture into the stack, which is how the region is looked at without
// having to make the panel refuse something first.
const messagesMockKey = "M"

// messagesCap is how far back the stack goes, since nobody reads the hundredth refusal and the
// panel holds these for as long as it runs.
const messagesCap = 50

// messagesRegion is the region as it is drawn under the record, given the record and the legend
// it sits between so it can tell whether the pane has the rows to spare for it.
func (m model) messagesRegion(record, help string) string {
	if !m.isMessagesDrawn(record, help) {
		return ""
	}

	block := padRows(strings.Join(m.messagesSlot(), "\n")+"\n", messagesRows)
	if record == "" {
		block = m.detailsRule() + block
	}

	return block + m.detailsRule()
}

// isMessagesDrawn is whether the region's rows come out of the list without taking it below a
// list worth reading, which is what stops a short pane drawing its chrome past its own bottom.
func (m model) isMessagesDrawn(record, help string) bool {
	if m.height <= 0 || !m.isMessagesRegion() {
		return false
	}

	return m.spareLines(record, help)-messagesHeaderRows-messagesRegionRows >= messagesListFloor
}

// messagesHeaderRows is what the title, the blank under it, and the filter take above the list,
// which the region is measured against because it is drawn below all three.
const messagesHeaderRows = 3

// messagesListFloor is how many rows the list keeps once the region has taken its own.
const messagesListFloor = 2

// messagesSlot is the message the cursor is on said in the rows the region always holds: what
// happened, as much of the reason as one line carries, and the way into the rest of it.
func (m model) messagesSlot() []string {
	if len(m.messages.items) == 0 {
		return []string{
			m.messagesSummaryRow(kindIcon(messageSuccess)+" No messages", kindStyle(messageSuccess)),
			dimStyle.Render(clip("  Weave is running normally", max(m.lineWidth(), 1))),
		}
	}

	held := m.messages.items[m.messagesCursor()]

	return []string{
		m.messagesSummaryRow(kindIcon(held.kind)+" "+held.summary, m.messagesSummaryStyle(held)),
		dimStyle.Render(clip("  "+held.detail, max(m.lineWidth(), 1))),
		dimStyle.Render(clip("  "+m.messagesHint(), max(m.lineWidth(), 1))),
	}
}

// messagesTallyGap is the least that stands between the summary and the tally, so the two never
// read as one word.
const messagesTallyGap = 1

// messagesSummaryRow is the summary with its tally pushed to the far side of the region, built
// from the tally backwards: what the tally takes is settled first and the summary is clipped
// against what is left, since a row a single column wider than the pane wraps and the region
// stops being the fixed three rows the rest of the panel is laid out against.
func (m model) messagesSummaryRow(summary string, style lipgloss.Style) string {
	width := max(m.lineWidth(), 1)

	tally := m.messagesTally()
	if len(tally)+messagesTallyGap >= width {
		tally = ""
	}

	room := width
	if tally != "" {
		room -= len(tally) + messagesTallyGap
	}

	clipped := clip(summary, max(room, 1))
	row := style.Render(clipped)
	if tally == "" {
		return row
	}

	pad := width - lipgloss.Width(clipped) - len(tally)

	return row + strings.Repeat(" ", pad) + m.messagesTallyStyle().Render(tally)
}

func (m model) messagesSummaryStyle(held message) lipgloss.Style {
	if m.messages.focused {
		return kindStyle(held.kind).Bold(true)
	}

	return kindStyle(held.kind)
}

func (m model) messagesHint() string {
	if m.messages.focused {
		return "‹ › history · esc dismiss"
	}

	return messagesFocusKey + " to read"
}

// messagesTally is the count on the right of the summary, which says how much is behind this
// message while the list has the keys and where in the stack the reader is once it does not.
func (m model) messagesTally() string {
	count := len(m.messages.items)
	if count == 0 {
		return ""
	}

	if m.messages.focused {
		return fmt.Sprintf("%d of %d", m.messagesCursor()+1, count)
	}

	if count < 2 {
		return ""
	}

	return fmt.Sprintf("+%d", count-1)
}

// messagesTallyStyle is where the region says the keys are its own, since it has no title of its
// own to draw in the colour every other focused section is drawn in.
func (m model) messagesTallyStyle() lipgloss.Style {
	if m.messages.focused {
		return headerStyle
	}

	return dimStyle
}

// pushMessage puts a message at the front of the stack, which is where the region draws from, and
// keeps the reader on whatever they were reading when it lands.
func (m *model) pushMessage(next message) {
	if next.detail == "" {
		next.detail = "no reason given"
	}
	if next.at.IsZero() {
		next.at = time.Now()
	}

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

// messagesDoc is the whole of the message the cursor is on, drawn in the block beside the list
// while the region holds the keys, so the reason is read at the width of the window rather than
// folded into the three rows the region keeps.
func (m model) messagesDoc() string {
	if len(m.messages.items) == 0 {
		return "_No messages_\n\nWeave is running normally."
	}

	held := m.messages.items[m.messagesCursor()]

	var b strings.Builder
	fmt.Fprintf(&b, "_Message %d of %d · %s · %s_\n\n", m.messagesCursor()+1, len(m.messages.items), held.at.Format("3:04pm"), kindLabel(held.kind))
	fmt.Fprintf(&b, "%s %s\n\n%s\n", kindIcon(held.kind), held.summary, held.detail)

	if held.hint != "" {
		fmt.Fprintf(&b, "\n%s\n", held.hint)
	}

	if held.trace != "" {
		fmt.Fprintf(&b, "\n`weave trace %s`\n", held.trace)
	}

	return b.String()
}

func messagesColumn() columnSpec {
	return columnSpec{floor: columnFloor, doc: model.messagesDoc}
}

// messagesBlock is the message drawn in the block beside the list, scrolled to where the reader
// left it, which is asked for directly because a form keeps the column spec while it is up.
func (m model) messagesBlock(split columnSplit) docWindow {
	return docWindow{
		rows:    docRows(m.messagesDoc(), split.column),
		top:     m.column.top,
		markers: true,
	}
}

func (m model) messagesCursor() int {
	return clampCursor(m.messages.cursor, len(m.messages.items))
}

// isMessagesRegion is whether the screen on top draws the region at all, which the review screen
// does not because it composes its own record and legend.
func (m model) isMessagesRegion() bool {
	return m.isDetailsScreen() && m.screen != screenReview
}

// isMessagesFocused is whether the region holds the keys, which it cannot on a screen that does
// not draw it, so the focus goes dormant there rather than following the reader onto it.
func (m model) isMessagesFocused() bool {
	return m.messages.focused && m.isMessagesRegion()
}

// messagesChrome is the record and the legend the region sits between, which is what says how
// many rows the list has left to give it, drawn from the screen rather than passed down because
// the key router and the layout have neither to hand.
func (m model) messagesChrome() (string, string) {
	help := m.screen.spec().help
	if help == nil {
		return "", ""
	}

	return m.detailsView(), m.renderHelp(help(m))
}

// canHoldMessages is whether the region has the rows to be drawn where the panel stands now, and
// so whether there is anything for it to hold the keys on behalf of.
func (m model) canHoldMessages() bool {
	return m.isMessagesDrawn(m.messagesChrome())
}

// settleMessagesFocus hands the keys back when the region no longer has the rows to draw itself,
// since a region holding the keys off a pane that draws nothing is a list whose arrows have gone
// dead with nothing on screen saying why.
func (m model) settleMessagesFocus() model {
	if !m.messages.focused || m.canHoldMessages() {
		return m
	}

	// Growing the pane back does not take the keys again, since the reader asked for the region
	// once and did not ask for it a second time.

	m.messages.focused = false
	m.messages.cursor = 0
	m.column.top = 0

	m.reconcileSolo()
	m.restageScreen()

	return m
}

// toggleMessagesFocus moves the keys between the list and the region, which is also what puts the
// message in the block beside the list, since the block belongs to whoever holds the keys.
func (m model) toggleMessagesFocus() model {
	// Nothing to read is nothing to hold the keys for, the way the record refuses them with no
	// fields under it, and a pane too short to draw the region is nothing to read on.
	if !m.messages.focused && (len(m.messages.items) == 0 || !m.canHoldMessages()) {
		return m
	}

	m.messages.focused = !m.messages.focused
	m.details.focused = false
	m.messages.cursor = 0
	m.column.top = 0

	m.reconcileSolo()
	m.restageScreen()

	return m
}

func (m model) updateMessages(key tea.KeyMsg) model {
	// Routed here rather than left to the global keys, since the region takes every key while it
	// holds them and the message it is showing is the thing being scrolled.
	if direction, ok := m.isColumnScroll(key.String()); ok {
		return m.scrollColumn(direction)
	}

	switch key.String() {
	case messagesFocusKey:
		return m.toggleMessagesFocus()
	case "esc":
		return m.dismissMessage()
	case ">", "]":
		m.messages.cursor = clampCursor(m.messagesCursor()+1, len(m.messages.items))
		m.column.top = 0
	case "<", "[":
		m.messages.cursor = clampCursor(m.messagesCursor()-1, len(m.messages.items))
		m.column.top = 0
	case messagesMockKey:
		return m.pushMockMessage()
	}

	return m
}

// dismissMessage drops the message on screen and lands on the one behind it, handing the keys back
// to the list once the stack is empty, since a region holding nothing is a dead end.
func (m model) dismissMessage() model {
	if len(m.messages.items) == 0 {
		return m.toggleMessagesFocus()
	}

	cursor := m.messagesCursor()
	m.messages.items = slices.Delete(slices.Clone(m.messages.items), cursor, cursor+1)
	m.messages.cursor = clampCursor(cursor, len(m.messages.items))
	m.column.top = 0

	if len(m.messages.items) == 0 {
		return m.toggleMessagesFocus()
	}

	m.restageScreen()

	return m
}

// pushMockMessage fills the region from the fixtures, which is how the three states are looked at
// without waiting for the panel to be refused something.
func (m model) pushMockMessage() model {
	m.pushMessage(mockMessages[len(m.messages.items)%len(mockMessages)])
	m.column.top = 0
	m.restageScreen()

	return m
}

func (m model) messagesHelp() keyMap {
	history := binding([]string{"<", ">"}, "history")
	dismiss := binding([]string{"esc"}, "dismiss")
	back := binding([]string{messagesFocusKey}, "back to list")

	return keyMap{
		short: []key.Binding{history, dismiss, back, m.helpBinding()},
		full:  []key.Binding{history, dismiss, back, m.columnScrollBinding("the message"), m.helpBinding()},
	}
}

// messagesBinding is the region's own key as the legend names it, which says what pressing it does
// from where the reader is rather than what the region is called.
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

func kindStyle(kind messageKind) lipgloss.Style {
	switch kind {
	case messageError:
		return lipgloss.NewStyle().Foreground(colorDanger)
	case messageWarning:
		return lipgloss.NewStyle().Foreground(colorWarning)
	case messageSuccess:
		return lipgloss.NewStyle().Foreground(colorSuccess)
	case messageInfo:
		return dimStyle
	}

	return dimStyle
}

// mockMessages are the fixtures the region is drawn from while nothing real reports through it,
// long enough in the reason to show what the three rows do with one that does not fit.
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
