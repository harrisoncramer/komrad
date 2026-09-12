package pane

import (
	"context"
	"fmt"

	"weave/internal/harness"
)

// Observe reads the frame the agent's pane is drawing through the harness that drew it.
func (p *Pane) Observe(_ context.Context, target harness.Target) (harness.Observation, error) {
	frame := p.terminal.CapturePane(target.Pane)
	if frame == "" {
		return harness.Observation{}, nil
	}

	input, err := p.readFrame(target.HarnessID, frame)
	if err != nil {
		return harness.Observation{}, err
	}

	return harness.Observation{
		Activity: input.Activity,
		Held:     input.Held,
		Frame:    frame,
	}, nil
}

// readFrame is what the named harness reads out of one capture.
func (p *Pane) readFrame(harnessID, frame string) (harness.Input, error) {
	found, err := p.harnesses.GetByID(harnessID)
	if err != nil {
		return harness.Input{}, fmt.Errorf("failed to find the harness reading a pane: %w", err)
	}

	return found.ReadInput(frame), nil
}

// readInput captures the pane and reads it, which is the whole of an observation minus the
// frame itself.
func (p *Pane) readInput(target harness.Target) (harness.Input, error) {
	return p.readFrame(target.HarnessID, p.terminal.CapturePane(target.Pane))
}
