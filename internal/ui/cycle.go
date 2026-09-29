package ui

import (
	"slices"

	"komrad/internal/ui/layout"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// stop is a place that holds the keys and shows something about the hovered row.
type stop int

const (
	stopList stop = iota
	stopAgent
	stopThread
	stopRecord
	// stopTerminal is the shell opened in the pane below the stage.
	stopTerminal
	// stopDocument is the document drawn beside the documents list, or the diff of one of its writes.
	stopDocument
	// stopLog is the log of the source hovered on the logs screen, drawn beside the list.
	stopLog
	// stopOther is a pane beside the sidebar that is not a stop.
	stopOther
)

func (m model) isCycleKey(pressed string) bool {
	return isBound(pressed, m.keys.Cycle.Views)
}

func (m model) cycleBinding() key.Binding {
	return binding(m.keys.Cycle.Views, "next/prev view")
}

// areCycleKeysBound reports whether tmux sends the cycle keys to the panel from every pane.
func (m model) areCycleKeysBound() bool {
	return m.session != nil && m.frame != nil && m.hasPaneStops()
}

// holdCycleKeys binds the cycle keys while an agent's pane or a shell is beside the sidebar, and
// gives them back when there is no pane for them to reach.
func (m model) holdCycleKeys() {
	if m.session == nil || m.frame == nil {
		return
	}

	m.session.HoldCycleKeys(m.settings.Pane, m.hasPaneStops())
}

// findPressedElsewhere finds the stop a cycle key was pressed in when tmux sent it from a pane beside
// the sidebar.
func (m model) findPressedElsewhere(pressed string) (stop, bool) {
	if !m.areCycleKeysBound() || !m.isCycleKey(pressed) {
		return stopList, false
	}

	active := m.tmux.GetActivePane(m.settings.Pane)
	switch active {
	case "", m.settings.Pane:
		return stopList, false
	case m.focusPane():
		return stopAgent, true
	case m.terminalPane():
		return stopTerminal, true
	}

	return stopOther, true
}

// cycleFromElsewhere takes the keys back into the sidebar, leaving a search being typed where it
// stands.
func (m model) cycleFromElsewhere(pressed string, from stop) (tea.Model, tea.Cmd) {
	if m.isTyping() {
		m.tmux.SelectPane(m.settings.Pane)

		return m, nil
	}

	if m.isMessagesFocused() {
		m = m.toggleMessagesFocus()
	}

	m.keysElsewhere = true

	return m.cycleView(pressed, from)
}

// readSidebarStop reads the stop holding the keys inside the sidebar.
func (m model) readSidebarStop() stop {
	switch {
	case m.isDetailsFocused():
		return stopRecord
	case m.screen == screenReview && m.review.isThreadTaken():
		return stopThread
	case m.screen == screenDocuments && m.documents.isBlockFocused(m.shared()):
		return stopDocument
	case m.screen == screenLogs && m.logs.isLogFocused(m.shared()):
		return stopLog
	}

	return stopList
}

// listStops lists the screen's stops in forward order, leaving out any it is not drawing.
func (m model) listStops() []stop {
	stops := []stop{stopList}

	switch {
	case m.screen == screenReview:
		if _, ok := m.review.diff.threadAtCursor(); ok {
			stops = append(stops, stopThread)
		}
	case m.screen == screenDocuments:
		if m.documents.isDiffDrawn(m.shared()) || m.documents.isReadDrawn(m.shared()) {
			stops = append(stops, stopDocument)
		}
	case m.screen == screenLogs:
		if m.logs.isLogDrawn(m.shared()) {
			stops = append(stops, stopLog)
		}
	case m.focusPane() != "":
		stops = append(stops, stopAgent)
	}

	if m.terminalPane() != "" {
		stops = append(stops, stopTerminal)
	}

	if m.isDetailsDrawn(m.hoveredDetails()) {
		stops = append(stops, stopRecord)
	}

	return stops
}

// cycleView moves the keys one stop on from where they are, or one back for the backward key.
func (m model) cycleView(pressed string, from stop) (tea.Model, tea.Cmd) {
	stops := m.listStops()
	back := pressed == m.keys.Cycle.Views[1]

	at := slices.Index(stops, from)
	switch {
	case at < 0 && back:
		at = len(stops) - 1
	case at < 0:
		at = 0
	case back:
		at = (at + len(stops) - 1) % len(stops)
	default:
		at = (at + 1) % len(stops)
	}

	return m.enterStop(stops[at])
}

func (m model) enterStop(to stop) (tea.Model, tea.Cmd) {
	switch to {
	case stopAgent:
		m.tmux.SelectPane(m.focusPane())
	case stopTerminal:
		m.tmux.SelectPane(m.terminalPane())
	case stopRecord:
		return m.enterSidebar(to).jumpIntoDetails()
	case stopThread:
		m = m.enterSidebar(to).blurDetails()
		m.review.threads.focus = true
	case stopDocument:
		m = m.enterSidebar(to).blurDetails()
		m.documents.focusRead(m.shared())
	case stopLog:
		m = m.enterSidebar(to).blurDetails()
		m.logs.logFocus = true
	case stopList, stopOther:
		m = m.enterSidebar(to).blurDetails()
	}

	return m, nil
}

func (m model) enterSidebar(to stop) model {
	if m.keysElsewhere {
		m.tmux.SelectPane(m.settings.Pane)
		m.keysElsewhere = false
	}

	m.blurTabs()
	m.blurKinds()
	if m.screen == screenReview && to != stopThread {
		m.review.threads.blur()
	}

	if to != stopDocument {
		m.documents.focus = false
	}

	if to != stopLog {
		m.logs.logFocus = false
	}

	return m
}

// terminalPane is the shell the cycle stops at, empty while none is open.
func (m model) terminalPane() string {
	if m.frame == nil || m.info.kind != infoTerminal {
		return ""
	}

	return m.frame.Pane(layout.RegionInfo)
}

func (m model) hasPaneStops() bool {
	return m.focusPane() != "" || m.terminalPane() != ""
}
