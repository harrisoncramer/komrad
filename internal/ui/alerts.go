package ui

import (
	"fmt"
	"time"

	"weave/internal/errui"

	tea "github.com/charmbracelet/bubbletea"
)

// alertHistoryCap bounds how many dismissed alerts the history screen keeps, since a panel
// that runs for weeks would otherwise hold every alert it ever showed.
const alertHistoryCap = 100

// infoAlertLifetime is how long an info alert stays before it expires on its own. Warn and
// error carry no lifetime here at all: they stay until esc dismisses them, since those are
// what a user must not miss.
const infoAlertLifetime = 6 * time.Second

// queuedAlert is one alert in the queue, carrying the sequence number its expiry tick is
// named for and the count of times an identical one has coalesced into it.
type queuedAlert struct {
	errui.Alert
	seq   int
	count int
}

// alertExpiredMsg is a tick naming the alert it is for. A tick that lands after its alert
// already left the queue some other way names nothing there anymore and is dropped, rather
// than expiring whatever the newest happens to be by then.
type alertExpiredMsg struct {
	seq int
}

// alertQueue is the panel's alerts: the newest at the front, where it renders, whatever else
// is queued behind it, and a bounded history of what has left the queue either way, so an
// alert that expired unread is still somewhere to find.
type alertQueue struct {
	queue   []queuedAlert
	history []queuedAlert
	nextSeq int
}

// push adds an alert as the newest, coalescing it into an identical one already queued
// rather than stacking a duplicate. It returns the tick that expires it when the level is
// info, and nil for a level that stays until dismissed. The hint defaults to what Render
// itself falls back to; a caller wanting another one sets it after with setNewestHint.
func (q *alertQueue) push(level errui.Level, summary, detail string) tea.Cmd {
	entry := queuedAlert{Alert: errui.Alert{Level: level, Summary: summary, Detail: detail}, count: 1}

	for i, existing := range q.queue {
		if existing.Summary == summary && existing.Detail == detail {
			entry.count = existing.count + 1
			q.queue = append(q.queue[:i], q.queue[i+1:]...)

			break
		}
	}

	q.nextSeq++
	entry.seq = q.nextSeq
	q.queue = append([]queuedAlert{entry}, q.queue...)

	if level != errui.LevelInfo {
		return nil
	}

	seq := entry.seq

	return tea.Tick(infoAlertLifetime, func(time.Time) tea.Msg { return alertExpiredMsg{seq: seq} })
}

// dismissNewest moves the alert on screen to history, revealing whatever was queued behind
// it. It is a no-op with nothing queued.
func (q *alertQueue) dismissNewest() {
	if len(q.queue) == 0 {
		return
	}

	q.moveToHistory(0)
}

// expire drops the alert a tick names into history, wherever it sits in the queue. A tick
// for an alert that already left some other way names nothing here anymore and does
// nothing.
func (q *alertQueue) expire(seq int) {
	for i, a := range q.queue {
		if a.seq == seq {
			q.moveToHistory(i)

			return
		}
	}
}

// moveToHistory takes the alert at i out of the queue and into history, oldest dropped once
// the bound is reached.
func (q *alertQueue) moveToHistory(i int) {
	dismissed := q.queue[i]
	q.queue = append(q.queue[:i], q.queue[i+1:]...)

	q.history = append([]queuedAlert{dismissed}, q.history...)
	if len(q.history) > alertHistoryCap {
		q.history = q.history[:alertHistoryCap]
	}
}

// setNewestHint changes the hint on the alert on screen, for a form that wants to say how
// its own refusal goes away rather than take the default. A no-op with nothing queued.
func (q *alertQueue) setNewestHint(hint string) {
	if len(q.queue) == 0 {
		return
	}

	q.queue[0].Hint = hint
}

// newest is the alert on screen and how many are queued behind it, or false with nothing
// queued. A coalesced alert's summary carries the count, since a repeated failure collapsing
// silently into one line would read as though it only happened once.
func (q alertQueue) newest() (errui.Alert, int, bool) {
	if len(q.queue) == 0 {
		return errui.Alert{}, 0, false
	}

	front := q.queue[0]
	alert := front.Alert
	if front.count > 1 {
		alert.Summary = fmt.Sprintf("%s (×%d)", alert.Summary, front.count)
	}

	return alert, len(q.queue) - 1, true
}

// newestAlert is the alert on screen, or false with nothing queued.
func (m model) newestAlert() (errui.Alert, bool) {
	alert, _, ok := m.alerts.newest()

	return alert, ok
}

// alertChrome renders whatever the alert queue shows: the alert on screen, and if others are
// queued behind it, one dim line counting them rather than stacking them, since chrome eats
// rows the list below it needs.
func (m model) alertChrome() string {
	alert, behind, ok := m.alerts.newest()
	if !ok {
		return ""
	}

	chrome := alert.Render(m.width)
	if behind > 0 {
		chrome += dimStyle.Render(fmt.Sprintf("  %d more queued, esc to dismiss this one", behind)) + "\n"
	}

	return chrome
}

// enterAlertHistory switches to the alert history, the dismissed and expired alerts kept
// after they leave the queue.
func (m model) enterAlertHistory() (tea.Model, tea.Cmd) {
	m.enterScreen(screenAlerts)
	m.alertHistory.cursor = clampCursor(m.alertHistory.cursor, len(m.alerts.history))

	return m, nil
}

// alertHistoryState is the alert history screen, just where the cursor rests, since the
// alerts themselves live on the queue they came from.
type alertHistoryState struct {
	cursor int
}

// updateAlerts drives the alert history screen. Nav moves the cursor and there is nothing
// else here to act on, since history is read-only by nature.
func (m model) updateAlerts(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()

	switch {
	case isBound(k, m.keys.Alerts.Back):
		return m.returnToAgents()
	case isBound(k, m.keys.Nav.Up):
		if m.alertHistory.cursor > 0 {
			m.alertHistory.cursor--
		}
	case isBound(k, m.keys.Nav.Down):
		if m.alertHistory.cursor < len(m.alerts.history)-1 {
			m.alertHistory.cursor++
		}
	}

	return m, nil
}

// alertHistoryRow renders one history entry as a single row, its level's marker and summary
// followed by its detail dimmed, the way every other list here keeps one row to one line so
// the height a screen budgets for its rows holds.
func alertHistoryRow(a queuedAlert, hovered bool) string {
	prefix := "  "
	if hovered {
		prefix = cursorStyle.Render("› ")
	}

	summary := a.Summary
	if a.count > 1 {
		summary = fmt.Sprintf("%s (×%d)", summary, a.count)
	}

	line := prefix + a.Level.Style().Render(a.Level.Marker()+" "+summary)
	if a.Detail != "" {
		line += "  " + dimStyle.Render(a.Detail)
	}

	return line
}

// alertsView renders the alert history screen, the dismissed and expired alerts kept after
// they leave the queue.
func (m model) alertsView() string {
	header := headerStyle.Render("Alert History") + "\n\n"
	help := "\n" + m.renderHelp(m.alertsHelp())

	if len(m.alerts.history) == 0 {
		return header + dimStyle.Render("nothing dismissed yet") + "\n" + help
	}

	rows := make([]string, 0, len(m.alerts.history))
	for i, a := range m.alerts.history {
		rows = append(rows, alertHistoryRow(a, i == m.alertHistory.cursor))
	}

	return header + renderList(rows, m.alertHistory.cursor, m.listLines(header, help)) + help
}
