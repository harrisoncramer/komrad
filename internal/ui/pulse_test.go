package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPulseKeepsOneTickStream covers a blink starting while another is running, where a tick of
// its own would drive both of them at double speed.
func TestPulseKeepsOneTickStream(t *testing.T) {
	m := model{}
	second := pulse{}

	require.NotNil(t, m.startPulse(&m.messages.pulse, 5))
	assert.Nil(t, m.startPulse(&second, 5), "the tick already running serves both")

	ticks := 0
	for m.advancePulses() != nil {
		ticks++
	}

	assert.False(t, m.pulsing())
	assert.Positive(t, ticks)
}
