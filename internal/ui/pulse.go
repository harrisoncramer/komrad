package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// pulseInterval is how long each half of a blink stays on screen.
const pulseInterval = 150 * time.Millisecond

// pulse is a blink drawing attention to something that has just changed, counted in the halves
// it has left to run.
type pulse struct {
	left int
}

type pulseMsg struct{}

func pulseTick() tea.Cmd {
	return tea.Tick(pulseInterval, func(time.Time) tea.Msg { return pulseMsg{} })
}

// start blinks the given number of times, lit from the frame it is asked for.
func (p *pulse) start(blinks int) {
	p.left = blinks*2 - 1
}

// advance takes the blink to its next half.
func (p *pulse) advance() {
	if p.left > 0 {
		p.left--
	}
}

// isRunning reports whether the blink has halves left to draw.
func (p pulse) isRunning() bool {
	return p.left > 0
}

// isLit reports whether the blink is in its bright half.
func (p pulse) isLit() bool {
	return p.left%2 == 1
}

// isPulsing reports whether any of the panel's blinks is still running.
func (m model) isPulsing() bool {
	return m.messages.pulse.isRunning()
}

// startPulse sets a blink running, asking for a tick where nothing else is already driving one.
func (m *model) startPulse(blinking *pulse, blinks int) tea.Cmd {
	running := m.isPulsing()
	blinking.start(blinks)

	if running {
		return nil
	}

	return pulseTick()
}

// advancePulses takes every blink the panel is drawing to its next half.
func (m *model) advancePulses() tea.Cmd {
	m.messages.pulse.advance()

	if !m.isPulsing() {
		return nil
	}

	return pulseTick()
}
