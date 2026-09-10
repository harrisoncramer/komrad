package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

// start blinks the given number of times, lit from the frame it is asked for and asking for a
// tick unless one is already running.
func (p *pulse) start(blinks int) tea.Cmd {
	running := p.left > 0
	p.left = blinks*2 - 1

	if running {
		return nil
	}

	return pulseTick()
}

// advance takes the blink to its next half and asks for the one after it.
func (p *pulse) advance() tea.Cmd {
	if p.left == 0 {
		return nil
	}

	p.left--

	if p.left == 0 {
		return nil
	}

	return pulseTick()
}

// lit reports whether the blink is in its bright half.
func (p pulse) lit() bool {
	return p.left%2 == 1
}

// advancePulses takes every blink the panel is drawing to its next half.
func (m *model) advancePulses() tea.Cmd {
	return m.messages.pulse.advance()
}
