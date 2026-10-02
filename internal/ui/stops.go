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
	// stopPullRequest is the pull request drawn over the review's file tree.
	stopPullRequest
)

// listStops lists the screen's stops in forward order, leaving out any it is not drawing.
func (m *model) listStops() []stop {
	stops := []stop{stopList}

	switch {
	case m.isScreenBlockDrawn(), m.isDocReadable() && !m.isDormantHovered():
		stops = append(stops, stopBlock)
	case m.findFocusPane() != "":
		stops = append(stops, stopAgent)
	}

	if p, ok := m.active().(pullRequestFocuser); ok && p.isPullRequestDrawn(m) {
		stops = append(stops, stopPullRequest)
	}

	if m.findTerminalPane() != "" {
		stops = append(stops, stopTerminal)
	}

	if m.isDetailsDrawn(m.hoveredDetails()) {
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
		if b, ok := m.active().(columnFocuser); ok && m.isScreenBlockDrawn() {
			b.focusBlock(m)
		} else {
			m.focusDoc()
		}
	case stopPullRequest:
		m.enterSidebar(to)
		m.blurDetails()
		if p, ok := m.active().(pullRequestFocuser); ok {
			p.focusPullRequest(m)
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
	if p, ok := m.active().(pullRequestFocuser); ok && to != stopPullRequest {
		p.blurPullRequest()
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

	if p, ok := m.active().(pullRequestFocuser); ok && p.isPullRequestFocused(m) {
		return stopPullRequest
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
		return m.agents.openHovered(m)
	}

	stops := m.listStops()

	to := m.leftStop
	if to == at || to == stopPullRequest || !slices.Contains(stops, to) {
		to = stops[(slices.Index(stops, at)+1)%len(stops)]
	}

	if to == at {
		return nil
	}

	return m.enterStop(to)
}

// isDormantHovered reports whether the agents screen is drawing the saved screen of a dormant agent.
func (m *model) isDormantHovered() bool {
	if m.screen != screenAgents {
		return false
	}

	item, ok := m.agents.hoveredResting(m)

	return ok && !item.Alive
}

// isScreenBlockDrawn reports whether the screen is drawing a block of its own beside the list.
func (m *model) isScreenBlockDrawn() bool {
	b, ok := m.active().(columnFocuser)

	return ok && b.isBlockDrawn(m)
}

// findFocusPane finds the agent's pane the picker stops at, empty where the stage holds none or
// the panel is drawn over it.
func (m *model) findFocusPane() string {
	if m.screen != screenAgents || m.frame.IsSolo() {
		return ""
	}

	return m.frame.FocusPane()
}
