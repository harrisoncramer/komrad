package pane

import (
	"context"
	"fmt"

	"komrad/internal/harness"
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
		Frame:    frame,
	}, nil
}

// IsRunning reports whether the agent's CLI is still the program in its pane.
func (p *Pane) IsRunning(_ context.Context, target harness.Target) (bool, error) {
	return p.terminal.CheckPaneRunningCLI(target.Pane)
}

// DoesFrameShowHarness reports whether a pane holds a frame its harness reads as one of its own.
func (p *Pane) DoesFrameShowHarness(pane, harnessID string) bool {
	// A harness komrad cannot look up is taken at its word.
	found, err := p.harnesses.GetByID(harnessID)
	if err != nil {
		return true
	}

	switch found.ReadInput(p.terminal.CapturePane(pane)).Activity {
	case harness.ActivityWorking, harness.ActivityWaiting, harness.ActivityIdle, harness.ActivityComposing:
		return true
	case harness.ActivityUnknown, harness.ActivityUnrecognized:
		return false
	}

	return false
}

// readFrame is what the named harness reads out of one capture.
func (p *Pane) readFrame(harnessID, frame string) (harness.Input, error) {
	found, err := p.harnesses.GetByID(harnessID)
	if err != nil {
		return harness.Input{}, fmt.Errorf("failed to find the harness reading a pane: %w", err)
	}

	return found.ReadInput(frame), nil
}

// readInput captures the pane and reads what its harness makes of it.
func (p *Pane) readInput(target harness.Target) (harness.Input, error) {
	return p.readFrame(target.HarnessID, p.terminal.CapturePane(target.Pane))
}
