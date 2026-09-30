package ui

import (
	"slices"

	"komrad/internal/ui/layout"

	tea "charm.land/bubbletea/v2"
)

// stop is a place that holds the keys and shows something about the hovered row.
type stop int

const (
	stopList stop = iota
	stopAgent
	// stopBlock is the block a screen draws beside its list, the review's conversation, a document or a log.
	stopBlock
	stopRecord
	// stopTerminal is the shell opened in the pane below the stage.
	stopTerminal
)

// listStops lists the screen's stops in forward order, leaving out any it is not drawing.
func (m model) listStops() []stop {
	stops := []stop{stopList}

	switch {
	case m.isScreenBlockDrawn(), m.isDocReadable():
		stops = append(stops, stopBlock)
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

func (m model) enterStop(to stop) (tea.Model, tea.Cmd) {
	switch to {
	case stopAgent:
		m.tmux.SelectPane(m.focusPane())
	case stopTerminal:
		m.tmux.SelectPane(m.terminalPane())
	case stopRecord:
		return m.enterSidebar(to).jumpIntoDetails()
	case stopBlock:
		m = m.enterSidebar(to).blurDetails()
		if b, ok := m.active().(blocked); ok && m.isScreenBlockDrawn() {
			b.focusBlock(m.shared())
		} else {
			m.focusDoc()
		}
	case stopList:
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
	if b, ok := m.active().(blocked); ok && to != stopBlock {
		b.blurBlock()
	}
	if to != stopBlock {
		m.reading.focused = false
	}

	return m
}

// atStop is the stop inside the panel that holds the keys.
func (m model) atStop() stop {
	switch {
	case m.isDetailsFocused():
		return stopRecord
	case m.isDocFocused():
		return stopBlock
	}

	if b, ok := m.active().(blocked); ok && b.isBlockFocused(m.shared()) {
		return stopBlock
	}

	return stopList
}

// toggleStop hands the keys back to the stop they last left, or on to the next stop when there is
// none to go back to.
func (m model) toggleStop() (tea.Model, tea.Cmd) {
	if m.keysElsewhere {
		return m, nil
	}

	at := m.atStop()
	stops := m.listStops()

	to := m.leftStop
	if to == at || !slices.Contains(stops, to) {
		to = stops[(slices.Index(stops, at)+1)%len(stops)]
	}

	if to == at {
		return m, nil
	}

	if to != stopList {
		m.filter.typing = false
	}

	return m.enterStop(to)
}

// terminalPane is the shell the picker stops at, empty while none is open.
func (m model) terminalPane() string {
	if m.frame == nil || m.terminalDir == "" {
		return ""
	}

	return m.frame.Pane(layout.RegionInfo)
}

// isScreenBlockDrawn reports whether the screen is drawing a block of its own beside the list.
func (m model) isScreenBlockDrawn() bool {
	b, ok := m.active().(blocked)

	return ok && b.isBlockDrawn(m.shared())
}
