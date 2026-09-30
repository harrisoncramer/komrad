package ui

import (
	"slices"

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
	case m.isScreenBlockDrawn(), m.isDocReadable() && !m.isDormantHovered():
		stops = append(stops, stopBlock)
	case m.findFocusPane() != "":
		stops = append(stops, stopAgent)
	}

	if m.findTerminalPane() != "" {
		stops = append(stops, stopTerminal)
	}

	if m.isDetailsDrawn(m.hoveredDetails()) {
		stops = append(stops, stopRecord)
	}

	return stops
}

func (m model) enterStop(to stop) (tea.Model, tea.Cmd) {
	if to != stopList {
		m.filter.typing = false
	}

	switch to {
	case stopAgent:
		m.tmux.SelectPane(m.findFocusPane())
	case stopTerminal:
		m.tmux.SelectPane(m.findTerminalPane())
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

// toggleStop resumes the hovered dormant agent, or hands the keys back to the stop they last left,
// or on to the next stop when there is none to go back to.
func (m model) toggleStop() (tea.Model, tea.Cmd) {
	if m.keysElsewhere {
		return m, nil
	}

	at := m.atStop()
	if at == stopList && m.isDormantHovered() {
		cmd := m.agents.openHovered(&m, m.shared())

		return m, cmd
	}

	stops := m.listStops()

	to := m.leftStop
	if to == at || !slices.Contains(stops, to) {
		to = stops[(slices.Index(stops, at)+1)%len(stops)]
	}

	if to == at {
		return m, nil
	}

	return m.enterStop(to)
}

// isDormantHovered reports whether the agents screen is drawing the saved screen of a dormant agent.
func (m model) isDormantHovered() bool {
	if m.screen != screenAgents {
		return false
	}

	item, ok := m.agents.hoveredResting(m.shared())

	return ok && !item.Alive
}

// isScreenBlockDrawn reports whether the screen is drawing a block of its own beside the list.
func (m model) isScreenBlockDrawn() bool {
	b, ok := m.active().(blocked)

	return ok && b.isBlockDrawn(m.shared())
}

// findFocusPane finds the agent's pane the picker stops at, empty where the stage holds none or
// the panel is drawn over it.
func (m model) findFocusPane() string {
	if m.screen != screenAgents || m.frame.IsSolo() {
		return ""
	}

	return m.frame.FocusPane()
}
