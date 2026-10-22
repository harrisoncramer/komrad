package ui

import (
	"slices"

	"komrad/internal/ui/screen"

	tea "charm.land/bubbletea/v2"
)

// stop is a place that holds the keys and shows something about the hovered row.
type stop int

const (
	stopList stop = iota
	stopAgent
	// stopBlock is the block a screen renders beside its list, the review's conversation, a document or a log.
	stopBlock
	stopRecord
	// stopTerminal is the shell opened in the pane below the stage.
	stopTerminal
)

// listStops lists the screen's stops in forward order, leaving out any it is not rendering.
func (m *model) listStops() []stop {
	stops := []stop{stopList}

	switch {
	case m.isScreenBlockRendered(), m.isDocReadable() && !m.isDormantHovered():
		stops = append(stops, stopBlock)
	case m.findFocusPane() != "":
		stops = append(stops, stopAgent)
	}

	if m.findTerminalPane() != "" {
		stops = append(stops, stopTerminal)
	}

	if m.isDetailsRendered(m.hoveredDetails()) {
		stops = append(stops, stopRecord)
	}

	return stops
}

func (m *model) enterStop(to stop) tea.Cmd {
	if to != stopList {
		m.filter.typing = false
	}

	switch to {
	case stopAgent:
		m.tmux.SelectPane(m.findFocusPane())
	case stopTerminal:
		m.tmux.SelectPane(m.findTerminalPane())
	case stopRecord:
		m.enterSidebar(to)

		return m.jumpIntoDetails()
	case stopBlock:
		m.enterSidebar(to)
		m.blurDetails()
		if b, ok := m.active().(columnFocuser); ok && m.isScreenBlockRendered() {
			b.focusBlock(m)
		} else {
			m.focusDoc()
		}
	case stopList:
		m.enterSidebar(to)
		m.blurDetails()
	}

	return nil
}

func (m *model) enterSidebar(to stop) {
	if m.term.keysElsewhere {
		m.tmux.SelectPane(m.settings.Pane)
		m.term.keysElsewhere = false
	}

	m.blurTabs()
	m.blurKinds()
	if b, ok := m.active().(columnFocuser); ok && to != stopBlock {
		b.blurBlock()
	}
	if to != stopBlock {
		m.reading.focused = false
	}
}

// atStop is the stop inside the panel that holds the keys.
func (m *model) atStop() stop {
	switch {
	case m.isDetailsFocused():
		return stopRecord
	case m.isDocFocused():
		return stopBlock
	}

	if b, ok := m.active().(columnFocuser); ok && b.isBlockFocused(m) {
		return stopBlock
	}

	return stopList
}

// toggleStop resumes the hovered dormant agent, or hands the keys back to the stop they last left,
// or on to the next stop when there is none to go back to.
func (m *model) toggleStop() tea.Cmd {
	if m.term.keysElsewhere {
		return nil
	}

	at := m.atStop()
	if at == stopList && m.isDormantHovered() {
		return m.apply(m.agents.openHovered(m))
	}

	stops := m.listStops()

	to := m.leftStop
	if to == at || !slices.Contains(stops, to) {
		to = stops[(slices.Index(stops, at)+1)%len(stops)]
	}

	if to == at {
		return nil
	}

	return m.enterStop(to)
}

// isDormantHovered reports whether the agents screen is rendering the saved screen of a dormant agent.
func (m *model) isDormantHovered() bool {
	if m.screen != screen.Agents || m.agents.kind != agentsLive {
		return false
	}

	item, ok := m.agents.hoveredResting(m)

	return ok && !item.Alive
}

// isScreenBlockRendered reports whether the screen is rendering a block of its own beside the list.
func (m *model) isScreenBlockRendered() bool {
	b, ok := m.active().(columnFocuser)

	return ok && b.isBlockRendered(m)
}

// findFocusPane finds the agent's pane the picker stops at, empty where the stage holds none or
// the panel is rendered over it.
func (m *model) findFocusPane() string {
	if m.screen != screen.Agents || m.frame.IsSolo() {
		return ""
	}

	return m.frame.FocusPane()
}
