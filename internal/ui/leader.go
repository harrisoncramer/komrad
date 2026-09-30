package ui

import (
	"slices"

	"komrad/lib/xslice"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// leaderKey is the leader as the panel reads it.
const leaderKey = "ctrl+space"

// panePicker is the row of stops the leader opens to hand the keys to one of them.
type panePicker struct {
	open  bool
	stops []stop
	focus int
	// returnTo is the pane that held the keys when the picker opened.
	returnTo string
}

func (m model) holdLeader() {
	if m.session == nil {
		return
	}

	m.session.HoldLeader(m.settings.Pane)
}

// routeLeader answers the leader and the key after it.
func (m model) routeLeader(key tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	pressed := key.String()

	if !m.leading {
		if pressed != leaderKey {
			return m, nil, false
		}

		m.leading = true

		return m, nil, true
	}

	m.leading = false

	switch pressed {
	case "v":
		next, cmd := m.toggleVisual()

		return next, cmd, true
	case "p":
		if m.visual.isActive() {
			return m, nil, true
		}

		return m.openPicker(), nil, true
	}

	return m, nil, true
}

func (m model) toggleVisual() (tea.Model, tea.Cmd) {
	switch {
	case m.visual.freezing:
		return m, nil
	case m.visual.frozen != "":
		return m.thaw(), nil
	}

	m.picker = panePicker{}
	m.visual.freezing = true

	return m, m.freezeCmd()
}

func (m model) openPicker() model {
	returnTo := m.tmux.GetActivePane(m.settings.Pane)
	if returnTo != "" && returnTo != m.settings.Pane {
		m.tmux.SelectPane(m.settings.Pane)
	}

	stops := m.listStops()
	focus := slices.Index(stops, m.heldStop(returnTo))

	m.picker = panePicker{open: true, stops: stops, focus: max(focus, 0), returnTo: returnTo}

	return m
}

// heldStop is the stop holding the keys while pane is the active one.
func (m model) heldStop(pane string) stop {
	switch pane {
	case "", m.settings.Pane:
		return m.readSidebarStop()
	case m.focusPane():
		return stopAgent
	case m.terminalPane():
		return stopTerminal
	}

	return stopOther
}

func (m model) updatePicker(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	stops := m.picker.stops
	pressed := key.String()

	switch pressed {
	case tabNextKey:
		m.picker.focus = stepStop(m.picker.focus, 1, len(stops))
	case tabPrevKey:
		m.picker.focus = stepStop(m.picker.focus, -1, len(stops))
	case "enter":
		return m.landPicker(stops[m.picker.focus])
	case "esc":
		returnTo := m.picker.returnTo
		m.picker = panePicker{}

		if returnTo != "" && returnTo != m.settings.Pane {
			m.tmux.SelectPane(returnTo)
		}
	default:
		for _, to := range stops {
			if pressed == to.mark() {
				return m.landPicker(to)
			}
		}
	}

	return m, nil
}

func (m model) landPicker(to stop) (tea.Model, tea.Cmd) {
	m.picker = panePicker{}
	m.keysElsewhere = false

	return m.enterStop(to)
}

func (s stop) label() string {
	switch s {
	case stopAgent:
		return "agent"
	case stopBlock:
		return "block"
	case stopRecord:
		return "details"
	case stopTerminal:
		return "terminal"
	case stopList, stopOther:
	}

	return "list"
}

// mark is the key that lands the picker on a stop.
func (s stop) mark() string {
	return s.label()[:1]
}

// pickerRow is the picker drawn as a row of stops.
func (m model) pickerRow() stops {
	listed := m.picker.stops

	return stops{
		labels: xslice.Map(listed, stop.label),
		marks:  xslice.Map(listed, stop.mark),
		focus:  m.picker.focus,
		lit:    noStop,
	}
}

func (m model) pickerHelp() keyMap {
	bindings := append([]key.Binding{stopsMoveBinding("move"), binding([]string{"enter"}, "land"), binding([]string{"esc"}, "done")},
		xslice.Map(m.picker.stops, func(s stop) key.Binding { return binding([]string{s.mark()}, s.label()) })...)

	return keyMap{short: bindings, full: bindings}
}
