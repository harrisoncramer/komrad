package cli

import (
	"weave/internal/harness"
)

// paneCapturer hands back what a pane is showing.
type paneCapturer interface {
	CapturePane(pane string) string
}

// paneFrameReader answers what a captured pane frame says about the harness typed into it, which
// is the panel's half of the harness registry and reaches it as a dependency.
type paneFrameReader struct {
	panes     paneCapturer
	harnesses *harness.Registry
}

// DoesFrameShowHarness reports whether a pane holds a frame its harness reads as one of its own,
// which is what a harness that has started has yet to draw while the shell it was typed into is
// still on screen.
func (r paneFrameReader) DoesFrameShowHarness(pane, harnessID string) bool {
	// A harness weave cannot look up is taken at its word, since waiting on a frame nothing can
	// classify would wait out the cap every time.
	found, err := r.harnesses.GetByID(harnessID)
	if err != nil {
		return true
	}

	switch found.ReadActivity(r.panes.CapturePane(pane)) {
	case harness.ActivityWorking, harness.ActivityWaiting, harness.ActivityIdle, harness.ActivityComposing:
		return true
	case harness.ActivityUnknown, harness.ActivityUnrecognized:
		return false
	}

	return false
}
