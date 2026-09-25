package cli

import (
	"komrad/internal/harness"
)

// paneCapturer hands back what a pane is showing.
type paneCapturer interface {
	CapturePane(pane string) string
}

// paneFrameReader answers what a captured pane frame says about the harness typed into it.
type paneFrameReader struct {
	panes     paneCapturer
	harnesses *harness.Registry
}

// DoesFrameShowHarness reports whether a pane holds a frame its harness reads as one of its own,
// which a harness still starting up has yet to draw.
func (r paneFrameReader) DoesFrameShowHarness(pane, harnessID string) bool {
	// A harness komrad cannot look up is taken at its word.
	found, err := r.harnesses.GetByID(harnessID)
	if err != nil {
		return true
	}

	switch found.ReadInput(r.panes.CapturePane(pane)).Activity {
	case harness.ActivityWorking, harness.ActivityWaiting, harness.ActivityIdle, harness.ActivityComposing:
		return true
	case harness.ActivityUnknown, harness.ActivityUnrecognized:
		return false
	}

	return false
}
