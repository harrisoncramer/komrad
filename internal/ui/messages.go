package ui

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"komrad/internal/ui/cursor"
	"komrad/internal/ui/keys"
	"komrad/internal/ui/screen"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// messagesIndent is the gutter every row of a message is rendered behind, which is the cursor's.
const messagesIndent = 2

const messagesBodyIndent = messagesIndent + 2

// messagesCap is how far back the stack goes.
const messagesCap = 50

// messagesPulseBlinks is how many times the summary of a message blinks as it lands.
const messagesPulseBlinks = 2

const messagesFlashFor = 5 * time.Second

type message struct {
	kind    screen.Kind
	key     string
	summary string
	detail  string
	hint    string
	trace   string
	at      time.Time
	landed  int
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
func (m *model) pulseMessages(before int) tea.Cmd {
	if m.messages.landed == before {
		return nil
	}

	m.messages.flashing = true
	landed := m.messages.landed
	done := m.clock.After(messagesFlashFor, func(time.Time) tea.Msg { return messagesFlashDoneMsg{landed: landed} })

	return tea.Batch(m.startPulse(&m.messages.pulse, messagesPulseBlinks), done)
}

func (m *model) endMessagesFlash(msg messagesFlashDoneMsg) {
	if msg.landed == m.messages.landed {
		m.messages.flashing = false
	}
}

// isMessagesPulseOn reports whether the newest message is in the lit half of a blink.
func (m *model) isMessagesPulseOn() bool {
	return m.messages.pulse.isLit()
}

// isMessagesFlashing reports whether the newest message is flashing beside the engine.
func (m *model) isMessagesFlashing() bool {
	return m.messages.flashing && len(m.messages.items) > 0
}

func buildMessagesColumn() columnSpec {
	return columnSpec{floor: columnFloor}
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
func (m *model) copyMessage() tea.Cmd {
	if len(m.messages.items) == 0 {
		return nil
	}

	at := m.messages.clampedCursor()
	m.messages.flashed = m.messages.items[at].landed

	return tea.Batch(m.copyCmd(formatMessageText(m.messages.items[at])), m.flashRevertTick())
}

// pushMessage puts a message at the front of the stack and keeps the reader on whatever they
// were reading when it lands.
func (m *model) pushMessage(next message) {
	if m.findMessage(next.key) >= 0 {
		return
	}

	if next.detail == "" {
		next.detail = "no reason given"
	}

	if next.at.IsZero() {
		next.at = m.clock.Now()
	}

	m.messages.landed++
	next.landed = m.messages.landed
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

func (m *model) canReadMessages() bool {
	return !m.active().takesEveryKey()
}

func (m *model) isMessagesFocused() bool {
	return m.messages.focused && m.canReadMessages()
}

// toggleMessagesFocus opens the messages in the main pane, and closes them again.
func (m *model) toggleMessagesFocus() {
	if !m.messages.focused && len(m.messages.items) == 0 {
		return
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
}

func (m *model) clickMessages() tea.Cmd {
	if m.isViewsOpen() {
		m.closeViews()
	}

	if m.canReadMessages() {
		m.toggleMessagesFocus()
	}

	return nil
}

func (m *model) clickMessage(key string) tea.Cmd {
	at, err := strconv.Atoi(key)
	if err != nil {
		return nil
	}

	m.messages.cursor = cursor.Clamp(at, len(m.messages.items))

	return nil
}

func (m *model) updateMessages(key tea.KeyPressMsg) tea.Cmd {
	k := key.String()

	switch {
	case keys.IsBound(k, m.keys.Messages.Open), k == "esc":
		m.toggleMessagesFocus()

		return nil
	case keys.IsBound(k, m.keys.Messages.Dismiss):
		m.dismissMessage()

		return nil
	case keys.IsBound(k, m.keys.Messages.DismissAll):
		m.dismissAllMessages()

		return nil
	case keys.IsBound(k, m.keys.Details.Copy):
		return m.copyMessage()
	case keys.IsBound(k, m.keys.Nav.Up):
		m.messages.cursor = cursor.Clamp(m.messages.clampedCursor()-1, len(m.messages.items))
	case keys.IsBound(k, m.keys.Nav.Down):
		m.messages.cursor = cursor.Clamp(m.messages.clampedCursor()+1, len(m.messages.items))
	case keys.IsBound(k, m.keys.Nav.Top):
		m.messages.cursor = 0
	case keys.IsBound(k, m.keys.Nav.Bottom):
		m.messages.cursor = len(m.messages.items) - 1
	}

	return nil
}

// dismissMessage drops the message on screen and lands on the one behind it, handing the keys
// back to the list once the stack is empty.
func (m *model) dismissMessage() {
	if len(m.messages.items) == 0 {
		m.toggleMessagesFocus()

		return
	}

	at := m.messages.clampedCursor()
	m.removeMessage(at)
	m.messages.cursor = cursor.Clamp(at, len(m.messages.items))

	if len(m.messages.items) == 0 {
		m.toggleMessagesFocus()
	}
}

func (m *model) dismissAllMessages() {
	m.messages.items = nil
	m.messages.cursor = 0
	m.messages.flashing = false

	m.toggleMessagesFocus()
}

// dropMessage takes the message raised under a key off the stack once what it said stops being true.
func (m *model) dropMessage(key string) {
	at := m.findMessage(key)
	if at < 0 {
		return
	}

	m.removeMessage(at)
	m.messages.cursor = cursor.Clamp(m.messages.cursor, len(m.messages.items))

	if len(m.messages.items) == 0 && m.messages.focused {
		m.toggleMessagesFocus()
	}
}

// findMessage is where the message raised under a key sits on the stack, and -1 when none does.
func (m *model) findMessage(key string) int {
	if key == "" {
		return -1
	}

	return slices.IndexFunc(m.messages.items, func(held message) bool { return held.key == key })
}

// removeMessage takes the message at a place off the stack, ending the flash when it was the newest.
func (m *model) removeMessage(at int) {
	if at == 0 {
		m.messages.flashing = false
	}
	m.messages.items = slices.Delete(slices.Clone(m.messages.items), at, at+1)
}

func (m *model) messagesHelp() keyMap {
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
func (m *model) messagesBinding() key.Binding {
	desc := "messages"
	if m.isMessagesFocused() {
		desc = "back to the list"
	}

	return binding(m.keys.Messages.Open, desc)
}
