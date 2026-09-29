package ui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"komrad/internal/glyph"
	"komrad/internal/ui/cursor"
	"komrad/internal/ui/paint"
	"komrad/internal/ui/paint/label"
	"komrad/lib/xslice"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// messagesIndent is the gutter every row of a message is drawn behind, which is the cursor's.
const messagesIndent = 2

const messagesBodyIndent = messagesIndent + 2

// messagesMockKey pushes a fixture into the stack.
const messagesMockKey = "M"

// messagesCap is how far back the stack goes.
const messagesCap = 50

// messagesPulseBlinks is how many times the summary of a message blinks as it lands.
const messagesPulseBlinks = 2

const messagesFlashFor = 5 * time.Second

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
	flashed int
	// landed counts the messages pushed, and pulse blinks the newest of them as it arrives.
	landed     int
	pulse      pulse
	flashing   bool
	fromRecord bool
}

type messagesFlashDoneMsg struct {
	landed int
}

// pulseMessages blinks the summary when the stack has grown since the count it is given.
func (m model) pulseMessages(before int) (model, tea.Cmd) {
	if m.messages.landed == before {
		return m, nil
	}

	m.messages.flashing = true
	landed := m.messages.landed
	done := startTimer(messagesFlashFor, func(time.Time) tea.Msg { return messagesFlashDoneMsg{landed: landed} })

	return m, tea.Batch(m.startPulse(&m.messages.pulse, messagesPulseBlinks), done)
}

func (m model) endMessagesFlash(msg messagesFlashDoneMsg) model {
	if msg.landed == m.messages.landed {
		m.messages.flashing = false
	}

	return m
}

// isMessagesPulseOn reports whether the newest message is in the lit half of a blink.
func (m model) isMessagesPulseOn() bool {
	return m.messages.pulse.isLit()
}

func (m model) drawMessagesFlash(width int) (string, bool) {
	if !m.messages.flashing || len(m.messages.items) == 0 || width < 3 {
		return "", false
	}

	held := m.messages.items[0]
	words := lipgloss.NewStyle()
	if m.isMessagesPulseOn() {
		words = words.Foreground(paint.Panel.Color(pulseTone(held.kind)))
	}

	return paint.Panel.Style(kindTone(held.kind)).Render(kindIcon(m.glyphs, held.kind)) + " " + words.Render(paint.Clip(held.summary, width-2)), true
}

func (m model) drawMessagesTally() string {
	worst, count := messageInfo, 0
	for _, held := range m.messages.items {
		if held.kind < messageWarning {
			continue
		}

		worst = max(worst, held.kind)
		count++
	}

	if count == 0 {
		return ""
	}

	tally := paint.Panel.Style(kindTone(worst)).Render(kindIcon(m.glyphs, worst) + " " + strconv.Itoa(count))

	return tally + "  " + dimStyle.Render(headLabel(m.keys.Messages.Open))
}

func buildMessagesColumn() columnSpec {
	return columnSpec{floor: columnFloor}
}

func (m model) buildMessagesWindow(width, rows int) docWindow {
	body := max(width-messagesBodyIndent, 1)
	count := len(m.messages.items)
	at := m.messages.clampedCursor()

	drawn := []string{m.drawMessagesHeading(width, count, at), ""}
	if count == 0 {
		drawn = append(drawn, dimStyle.Render(strings.Repeat(" ", messagesIndent)+"Komrad is running normally"))
	}

	start, end := 0, 0
	for i := range m.messages.items {
		if i > 0 {
			drawn = append(drawn, "")
		}

		if i == at {
			start = len(drawn)
		}

		drawn = append(drawn, m.drawMessageRows(i, body)...)

		if i == at {
			end = len(drawn)
		}
	}

	doc := docWindow{rows: drawn, markers: true}
	shape := doc.shape()
	shape.Top = shape.Reveal(max(end-1, 0), rows)
	doc.top = shape.Reveal(start, rows)

	return doc
}

func (m model) drawMessagesHeading(width, count, at int) string {
	title := paint.Panel.Header().Render("messages")
	if count == 0 {
		return title
	}

	tally := label.OfCount(at+1, count)
	pad := max(width-lipgloss.Width(title)-len(tally), 1)

	return title + strings.Repeat(" ", pad) + dimStyle.Render(tally)
}

func (m model) drawMessageRows(at, width int) []string {
	held := m.messages.items[at]
	hovered := at == m.messages.clampedCursor()
	summary := lipgloss.NewStyle()
	switch {
	case m.messages.flashed == at+1:
		summary = flashFieldStyle(paint.Panel)
	case hovered:
		summary = summary.Bold(true)
	}

	indent := strings.Repeat(" ", messagesBodyIndent)
	icon := paint.Panel.Style(kindTone(held.kind)).Render(kindIcon(m.glyphs, held.kind))

	var rows []string
	for i, line := range paint.WrapRows(held.summary, width) {
		if i == 0 {
			rows = append(rows, m.messagesCursorPrefix(hovered)+icon+" "+summary.Render(line))

			continue
		}

		rows = append(rows, indent+summary.Render(line))
	}

	meta := []string{kindLabel(held.kind), label.Friendly(held.at)}
	if held.trace != "" {
		meta = append(meta, "komrad log "+held.trace)
	}

	for _, piece := range []struct {
		text  string
		style lipgloss.Style
	}{
		{text: held.detail, style: lipgloss.NewStyle()},
		{text: held.hint, style: dimStyle},
		{text: strings.Join(meta, separated), style: dimStyle},
	} {
		if piece.text == "" {
			continue
		}

		rows = append(rows, xslice.Map(paint.WrapRows(piece.text, width), func(line string) string {
			return indent + piece.style.Render(line)
		})...)
	}

	return rows
}

// messagesCursorPrefix is the gutter the cursor stands in, which is the record's.
func (m model) messagesCursorPrefix(hovered bool) string {
	if !hovered {
		return strings.Repeat(" ", messagesIndent)
	}

	return caretGlyph(m.blinkingCaret(true))
}

func formatMessageText(held message) string {
	parts := []string{held.summary, held.detail}
	if held.hint != "" {
		parts = append(parts, held.hint)
	}

	if held.trace != "" {
		parts = append(parts, "komrad log "+held.trace)
	}

	return strings.Join(parts, "\n\n")
}

// copyMessage puts the message under the cursor on the clipboard, flashing it the way the
// record flashes a field it has copied.
func (m model) copyMessage() (tea.Model, tea.Cmd) {
	if len(m.messages.items) == 0 {
		return m, nil
	}

	at := m.messages.clampedCursor()
	m.messages.flashed = at + 1

	return m, tea.Batch(m.copyCmd(formatMessageText(m.messages.items[at])), flashRevertTick())
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
		m.messages.cursor = m.messages.clampedCursor() + 1

		return
	}

	m.messages.cursor = 0
}

// setNewestHint says what to do about the message that has just been reported, which the write
// that reported it knows and the call that failed does not.
func (s *messagesState) setNewestHint(hint string) {
	if len(s.items) == 0 {
		return
	}

	s.items[0].hint = hint
}

func (s messagesState) clampedCursor() int {
	return cursor.Clamp(s.cursor, len(s.items))
}

func (m model) canReadMessages() bool {
	return !m.active().takesEveryKey()
}

func (m model) isMessagesFocused() bool {
	return m.messages.focused && m.canReadMessages()
}

// toggleMessagesFocus opens the messages in the main pane, and closes them again.
func (m model) toggleMessagesFocus() model {
	if !m.messages.focused && len(m.messages.items) == 0 {
		return m
	}

	entering := !m.messages.focused
	m.messages.focused = !m.messages.focused
	if entering {
		m.messages.fromRecord = m.details.focused
	}
	m.details.focused = !entering && m.messages.fromRecord
	m.blurTabs()
	m.blurKinds()

	if entering {
		m.messages.cursor = 0
	}

	m.reconcileSolo()
	m.restageScreen()

	return m
}

func (m model) updateMessages(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, m.keys.Messages.Open), k == "esc":
		return m.toggleMessagesFocus(), nil
	case isBound(k, m.keys.Messages.Dismiss):
		return m.dismissMessage(), nil
	case isBound(k, m.keys.Messages.DismissAll):
		return m.dismissAllMessages(), nil
	case k == messagesMockKey:
		return m.pushMockMessage(), nil
	case isBound(k, m.keys.Details.Copy):
		return m.copyMessage()
	case isBound(k, m.keys.Nav.Up):
		m.messages.cursor = cursor.Clamp(m.messages.clampedCursor()-1, len(m.messages.items))
	case isBound(k, m.keys.Nav.Down):
		m.messages.cursor = cursor.Clamp(m.messages.clampedCursor()+1, len(m.messages.items))
	case isBound(k, m.keys.Nav.Top):
		m.messages.cursor = 0
	case isBound(k, m.keys.Nav.Bottom):
		m.messages.cursor = len(m.messages.items) - 1
	}

	return m, nil
}

// dismissMessage drops the message on screen and lands on the one behind it, handing the keys
// back to the list once the stack is empty.
func (m model) dismissMessage() model {
	if len(m.messages.items) == 0 {
		return m.toggleMessagesFocus()
	}

	at := m.messages.clampedCursor()
	if at == 0 {
		m.messages.flashing = false
	}
	m.messages.items = slices.Delete(slices.Clone(m.messages.items), at, at+1)
	m.messages.cursor = cursor.Clamp(at, len(m.messages.items))

	if len(m.messages.items) == 0 {
		return m.toggleMessagesFocus()
	}

	return m
}

func (m model) dismissAllMessages() model {
	m.messages.items = nil
	m.messages.cursor = 0
	m.messages.flashing = false

	return m.toggleMessagesFocus()
}

// pushMockMessage pushes a message from the fixtures.
func (m model) pushMockMessage() model {
	m.pushMessage(mockMessages[len(m.messages.items)%len(mockMessages)])
	m.restageScreen()

	return m
}

func (m model) messagesHelp() keyMap {
	copyMessage := binding(m.keys.Details.Copy, "copy")
	dismiss := binding(m.keys.Messages.Dismiss, "dismiss")
	dismissAll := binding(m.keys.Messages.DismissAll, "dismiss all")
	back := binding(slices.Concat([]string{"esc"}, m.keys.Messages.Open), "back to list")

	return keyMap{
		short: []key.Binding{copyMessage, dismiss, dismissAll, back, m.helpBinding()},
		full:  []key.Binding{copyMessage, dismiss, dismissAll, back, m.moveBinding(), m.helpBinding()},
	}
}

// messagesBinding is the messages key as the legend names it.
func (m model) messagesBinding() key.Binding {
	desc := "messages"
	if m.isMessagesFocused() {
		desc = "back to the list"
	}

	return binding(m.keys.Messages.Open, desc)
}

func kindIcon(glyphs glyph.Set, kind messageKind) string {
	switch kind {
	case messageError:
		return glyphs.Spell(glyph.MessageError)
	case messageWarning:
		return glyphs.Spell(glyph.MessageWarning)
	case messageSuccess:
		return glyphs.Spell(glyph.MessageSuccess)
	case messageInfo:
		return glyphs.Spell(glyph.MessageInfo)
	}

	return glyphs.Spell(glyph.MessageInfo)
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

func kindTone(kind messageKind) paint.Tone {
	switch kind {
	case messageError:
		return paint.ToneDanger
	case messageWarning:
		return paint.ToneWarning
	case messageSuccess:
		return paint.ToneSuccess
	case messageInfo:
		return paint.ToneMuted
	}

	return paint.ToneMuted
}

// pulseTone is the colour a landing message's words blink in.
func pulseTone(kind messageKind) paint.Tone {
	if kind == messageInfo {
		return paint.ToneAccent
	}

	return kindTone(kind)
}

// mockMessages are the fixtures the region is drawn from while nothing real reports through it,
// long enough in the reason to show what the rows do with one that does not fit.
var mockMessages = []message{
	{
		kind:    messageError,
		summary: "Your editor could not be opened on that message",
		detail:  "fork/exec /Users/harry/.local/state/komrad/worktrees/01a086ae-d59b-769b-a8a0-202d4014bddd/bin/editor: no such file or directory",
		trace:   "b3d0117e",
	},
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
		detail:  "komrad-bughunt-job, iss-408-reviewer, test-2 and test are hidden from the sidebar. restore them from the archive screen with Z.",
	},
}
