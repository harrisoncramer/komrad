package pane

import (
	"komrad/internal/harness"
)

// terminal is what an observer reads off the tmux server.
type terminal interface {
	CapturePane(pane string) string
	CheckPaneRunningCLI(pane string) (bool, error)
}

// harnessGetter hands back the harness that reads a pane's frames.
type harnessGetter interface {
	GetByID(id string) (harness.Harness, error)
}

// NewPaneParams is what a pane observer is built over.
type NewPaneParams struct {
	Terminal  terminal
	Harnesses harnessGetter
}

// Pane reads an agent through the terminal it is running in.
type Pane struct {
	terminal  terminal
	harnesses harnessGetter
}

// NewPane builds the observer over the tmux server and the harness registry.
func NewPane(params NewPaneParams) *Pane {
	return &Pane{
		terminal:  params.Terminal,
		harnesses: params.Harnesses,
	}
}

var _ harness.Observer = (*Pane)(nil)
